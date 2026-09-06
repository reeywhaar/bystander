package feeds

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// proxio stands in for a relay: it takes `GET /proxy?url=…&token=…` and fetches the target.
//
// A real one, not a stub that returns canned bytes — the point of most of these tests is what
// the request looks like when it arrives, and a stub that does not actually relay cannot show
// that the thing which came back is the target's answer rather than the relay's.
func proxio(t *testing.T, token string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy" {
			w.Header().Set(proxyError, `{"error":"not the relay path"}`)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("token") != token {
			w.Header().Set(proxyError, `{"error":"bad token"}`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		target := r.URL.Query().Get("url")
		seen = append(seen, target)

		out, err := http.NewRequestWithContext(r.Context(), r.Method, target, nil)
		if err != nil {
			w.Header().Set(proxyError, `{"error":"bad url"}`)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Method and headers are inherited from the caller, which is what the relay documents.
		out.Header = r.Header.Clone()
		out.Header.Del("Accept-Encoding")
		res, err := http.DefaultClient.Do(out)
		if err != nil {
			w.Header().Set(proxyError, `{"error":"unreachable"}`)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		for k, vs := range res.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(res.StatusCode)
		buf := new(bytes.Buffer)
		buf.ReadFrom(res.Body)
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func relayAt(url, token string) *store.Proxy {
	return &store.Proxy{ID: "px_1", Kind: store.ProxioKind, URL: url, Token: token, Enabled: true}
}

func relayList(list ...*store.Proxy) Relays {
	return func(context.Context) ([]*store.Proxy, error) { return list, nil }
}

const feedXML = `<?xml version="1.0"?><rss version="2.0"><channel><title>Blocked Daily</title>
<item><guid>1</guid><title>A story</title><link>https://blocked.example/1</link></item>
</channel></rss>`

// A publisher that refuses this address is reached through a relay.
func TestABlockedFeedIsFetchedThroughARelay(t *testing.T) {
	var direct int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The relay reaches it from its own address, which is what the header stands in for.
		if r.Header.Get("X-Pretend-Relayed") == "" {
			direct++
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	defer origin.Close()

	proxy, seen := proxio(t, "sekrit")
	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(relayAt(proxy.URL, "sekrit"))
	// The stand-in relay marks what it forwards, so the origin can tell the two apart.
	f.client.Transport = marking{http.DefaultTransport, proxy.URL}

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if got.Parsed == nil || got.Parsed.Title != "Blocked Daily" {
		t.Fatalf("the relay did not bring back the feed: %+v", got)
	}
	if direct != 1 {
		t.Errorf("the publisher was asked directly %d times, want exactly one before the relay", direct)
	}
	if len(*seen) != 1 || (*seen)[0] != origin.URL {
		t.Errorf("the relay was asked for %v, want [%s]", *seen, origin.URL)
	}
}

// marking tags requests that go to the relay, so a single stand-in origin can tell a relayed
// request from a direct one without needing two addresses.
type marking struct {
	inner http.RoundTripper
	relay string
}

func (m marking) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.String(), m.relay) {
		req = req.Clone(req.Context())
		req.Header.Set("X-Pretend-Relayed", "1")
	}
	return m.inner.RoundTrip(req)
}

// A relay is only reached for after an answer that might be about who is asking.
func TestWhichFailuresReachForARelay(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   bool
	}{
		{http.StatusForbidden, true},
		{http.StatusUnavailableForLegalReasons, true},
		{http.StatusTooManyRequests, true},
		{http.StatusNotFound, false},
		{http.StatusGone, false},
		{http.StatusInternalServerError, false},
		{http.StatusOK, false},
		{http.StatusNotModified, false},
	} {
		var asked int
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			asked++
			w.WriteHeader(tc.status)
		}))
		proxy, seen := proxio(t, "t")
		f := NewFetcher("http://read.example.com")
		f.Proxies = relayList(relayAt(proxy.URL, "t"))
		f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
		origin.Close()

		if used := len(*seen) > 0; used != tc.want {
			t.Errorf("%d: relay used = %v, want %v (publisher asked %d times)",
				tc.status, used, tc.want, asked)
		}
	}
}

// Every relay is tried, in the order they are configured, and the first one that works wins.
func TestEachRelayIsTriedInTurn(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Pretend-Relayed") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	defer origin.Close()

	// Two relays that cannot work and one that can, so "the first that works" is a claim with
	// something to prove: a wrong token, an address nothing is listening on, and a good one.
	broken, brokenSeen := proxio(t, "the-right-token")
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // nothing is listening now
	good, goodSeen := proxio(t, "good")

	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(
		relayAt(broken.URL, "the-wrong-token"),
		relayAt(deadURL, "irrelevant"),
		relayAt(good.URL, "good"),
	)
	f.client.Transport = marking{http.DefaultTransport, good.URL}

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if got.Parsed == nil {
		t.Fatal("no relay got through, though the third could")
	}
	if len(*brokenSeen) != 0 {
		t.Errorf("the relay with the wrong token relayed anyway: %v", *brokenSeen)
	}
	if len(*goodSeen) != 1 {
		t.Errorf("the working relay was asked %d times, want 1", len(*goodSeen))
	}
}

// When nothing gets through, what is reported is the publisher's own answer.
//
// Not the last relay's. "The feed answered 403" is something an operator can act on; "the relay
// in Frankfurt answered 502" is about the relay, and it would be recorded against the feed and
// shown beside it in the interface as though the publisher had said it.
func TestWhenNoRelayWorksThePublishersAnswerIsReported(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("nope, not from there"))
	}))
	defer origin.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(relayAt(deadURL, "t"))

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded though nothing got through")
	}
	if got == nil || got.Status != http.StatusForbidden {
		t.Fatalf("reported %+v, want the publisher's 403", got)
	}
	if !strings.Contains(got.ErrorBody, "nope, not from there") {
		t.Errorf("the error body is %q, want what the publisher said", got.ErrorBody)
	}
}

// The token never leaves the relay's query string.
//
// Two ways it could. A relayed response's Request points at the relay, so anything reading
// res.Request.URL — which is where FinalURL comes from — would store `…?url=…&token=…` on the
// feed. And net/http wraps every transport failure in a *url.Error that prints the address it
// dialled, so one unreachable relay writes the credential into the log.
func TestTheTokenIsNotWrittenAnywhere(t *testing.T) {
	const token = "a-very-secret-token"

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Pretend-Relayed") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	defer origin.Close()

	proxy, _ := proxio(t, token)
	logged := new(bytes.Buffer)
	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(relayAt(proxy.URL, token))
	f.Log = slog.New(slog.NewJSONHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f.client.Transport = marking{http.DefaultTransport, proxy.URL}

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if strings.Contains(got.FinalURL, token) || strings.Contains(got.FinalURL, "/proxy") {
		t.Errorf("FinalURL is %q; it names the relay and carries the token", got.FinalURL)
	}
	if got.FinalURL != origin.URL {
		t.Errorf("FinalURL is %q, want the publisher's own address %q", got.FinalURL, origin.URL)
	}

	// And the same again with a relay that cannot be dialled at all, which is the path that
	// goes through the transport's error rather than through a response.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	f.Proxies = relayList(relayAt(deadURL, token))
	f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())

	if strings.Contains(logged.String(), token) {
		t.Errorf("the token was written to the log:\n%s", logged.String())
	}
	// The log should still be useful about it.
	if !strings.Contains(logged.String(), "relay") {
		t.Errorf("nothing about the relay reached the log at all:\n%s", logged.String())
	}
}

// A conditional request stays conditional through a relay, and the target is encoded whole.
func TestWhatArrivesAtTheRelay(t *testing.T) {
	target := "https://example.com/feed?format=rss&since=2026-01-01"
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", `W/"abc"`)
	req.Header.Set("User-Agent", "bystander/test")

	sent, err := through(req, relayAt("http://proxio.internal:80", "tok"))
	if err != nil {
		t.Fatalf("through(): %v", err)
	}

	if sent.URL.Path != "/proxy" {
		t.Errorf("path is %q, want /proxy", sent.URL.Path)
	}
	q := sent.URL.Query()
	// Percent-encoded and whole: a target with its own query string has to survive being
	// nested inside one, or the relay fetches a truncated address.
	if q.Get("url") != target {
		t.Errorf("url is %q, want %q", q.Get("url"), target)
	}
	if q.Get("token") != "tok" {
		t.Errorf("token is %q", q.Get("token"))
	}
	if q.Get("hide") != "1" {
		t.Error("hide is not set; the relay would forward this instance's own address")
	}
	// Inherited, which is what makes a 304 still possible through a relay.
	if sent.Header.Get("If-None-Match") != `W/"abc"` {
		t.Error("the conditional header did not survive")
	}
	if sent.Header.Get("User-Agent") != "bystander/test" {
		t.Error("the user agent did not survive")
	}
	// The original is untouched: it is what gets reported and what a retry would resend.
	if req.URL.String() != target {
		t.Errorf("the original request was rewritten to %q", req.URL)
	}
}

// A kind nothing knows how to dial is refused rather than guessed at.
func TestAnUnknownKindIsNotDialled(t *testing.T) {
	_, _, err := dial(
		&http.Client{},
		must(http.NewRequest(http.MethodGet, "https://example.com/feed", nil)),
		&store.Proxy{ID: "px_1", Kind: "carrier-pigeon", URL: "http://x:1080", Token: "t"})
	if err == nil {
		t.Fatal("a kind nothing knows was dialled anyway")
	}
	if !strings.Contains(err.Error(), "carrier-pigeon") {
		t.Errorf("the error does not say which kind: %v", err)
	}
}

// The two kinds are dialled differently, and that is the whole of what the kind decides.
func TestEachKindIsDialledItsOwnWay(t *testing.T) {
	req := must(http.NewRequest(http.MethodGet, "https://example.com/feed", nil))
	base := &http.Client{Timeout: 3 * time.Second}

	// proxio rewrites the request and reuses the client.
	sent, client, err := dial(base, req, relayAt("http://proxio:80", "t"))
	if err != nil {
		t.Fatalf("dial(proxio): %v", err)
	}
	if client != base {
		t.Error("proxio was given a client of its own; it needs nothing but a rewritten URL")
	}
	if sent.URL.Host != "proxio:80" || sent.URL.Path != "/proxy" {
		t.Errorf("proxio request went to %s", sent.URL)
	}

	// SOCKS5 leaves the request alone and changes what is underneath it.
	socks := &store.Proxy{ID: "px_2", Kind: store.SocksKind,
		URL: "socks5://socks:1080", Username: "u", Token: "p", Enabled: true}
	sent, client, err = dial(base, req, socks)
	if err != nil {
		t.Fatalf("dial(socks5): %v", err)
	}
	if sent.URL.String() != "https://example.com/feed" {
		t.Errorf("socks5 rewrote the request to %s; it should be untouched", sent.URL)
	}
	if client == base {
		t.Error("socks5 reused the ordinary client; it has to dial through its endpoint")
	}
	if client.Timeout != base.Timeout {
		t.Errorf("the socks client kept %v, want the caller's %v", client.Timeout, base.Timeout)
	}
}

// A client is kept per endpoint, and a changed credential gets a new one.
//
// The tempting key is the relay's id, and it is wrong exactly when it matters: correcting a
// password would go on using the old one out of the cache.
func TestASocksClientIsReusedButNotAcrossACredentialChange(t *testing.T) {
	socks := &store.Proxy{ID: "px_1", Kind: store.SocksKind,
		URL: "socks5://socks:1080", Username: "u", Token: "p", Enabled: true}

	cache := &clientCache{clients: map[string]*http.Client{}}
	first, err := cache.get(socks, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	again, err := cache.get(socks, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Error("the same endpoint built two clients; connections would never be reused")
	}

	corrected := *socks
	corrected.Token = "the-new-password"
	third, err := cache.get(&corrected, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("a corrected password reused the client built with the old one")
	}
}

// A proxy list that will not load leaves fetching working, directly.
func TestAnUnreadableProxyListDoesNotStopFetching(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer origin.Close()

	f := NewFetcher("http://read.example.com")
	f.Proxies = func(context.Context) ([]*store.Proxy, error) {
		return nil, context.DeadlineExceeded
	}
	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded against a 403")
	}
	if got == nil || got.Status != http.StatusForbidden {
		t.Fatalf("reported %+v, want the publisher's own 403", got)
	}
}

// scrub keeps what went wrong and loses where it was dialled.
func TestScrubKeepsTheCauseAndLosesTheAddress(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "http://proxio:80/proxy?url=https%3A%2F%2Fx&token=secret",
		Err: context.DeadlineExceeded,
	}
	if strings.Contains(scrub(err).Error(), "secret") {
		t.Errorf("scrub() kept the token: %v", scrub(err))
	}
	if !strings.Contains(scrub(err).Error(), "deadline") {
		t.Errorf("scrub() lost the cause: %v", scrub(err))
	}
}

// newTestLogger writes debug-level JSON into w, so a test can look for what did and did not
// reach the log.
func newTestLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func must(req *http.Request, err error) *http.Request {
	if err != nil {
		panic(err)
	}
	return req
}

// memory is a Routes that lives in a map, so the ladder can be tested without a database.
type memory struct {
	id       string
	at       time.Time
	reads    int
	writes   int
	forgets  int
	failRead bool
}

func (m *memory) ProxyRoute(context.Context, string) (string, time.Time, error) {
	m.reads++
	if m.failRead {
		return "", time.Time{}, context.DeadlineExceeded
	}
	return m.id, m.at, nil
}

func (m *memory) RememberProxyRoute(_ context.Context, _, id string) error {
	m.writes++
	m.id, m.at = id, time.Now()
	return nil
}

func (m *memory) ForgetProxyRoute(context.Context, string) error {
	m.forgets++
	m.id, m.at = "", time.Time{}
	return nil
}

// blocked is a publisher that only answers requests that came through the stand-in relay.
func blocked(t *testing.T, direct *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Pretend-Relayed") == "" {
			*direct++
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// What worked once is used straight away the next time, without asking the publisher again.
func TestWhatReachedAPublisherIsUsedFirstNextTime(t *testing.T) {
	var direct int
	origin := blocked(t, &direct)
	proxy, seen := proxio(t, "t")

	routes := &memory{}
	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(relayAt(proxy.URL, "t"))
	f.Routes = routes
	f.client.Transport = marking{http.DefaultTransport, proxy.URL}

	feed := &store.Feed{CanonicalURL: origin.URL}
	if _, err := f.Fetch(t.Context(), feed, time.Now()); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if direct != 1 {
		t.Fatalf("the publisher was asked directly %d times on the first fetch, want 1", direct)
	}
	if routes.writes != 1 || routes.id != "px_1" {
		t.Fatalf("the working relay was not remembered: %+v", routes)
	}

	// Three more fetches. Each one should go straight to the relay.
	for range 3 {
		if _, err := f.Fetch(t.Context(), feed, time.Now()); err != nil {
			t.Fatalf("later fetch: %v", err)
		}
	}
	if direct != 1 {
		t.Errorf("the publisher was asked directly %d times, want only the first", direct)
	}
	if len(*seen) != 4 {
		t.Errorf("the relay handled %d requests, want all four", len(*seen))
	}
}

// The ways a remembered route stops being used. In every one, the ladder starts from the top.
func TestWhenARememberedRouteIsIgnored(t *testing.T) {
	for _, tc := range []struct {
		what string
		// routes and relays are built per case, because the ones that matter need a live
		// relay that would have worked — otherwise the route fails for the wrong reason and
		// the test would pass with the rule it is checking deleted.
		build func(t *testing.T, relayURL string) (*memory, Relays)
	}{
		{
			"the relay it names has been deleted",
			func(_ *testing.T, relayURL string) (*memory, Relays) {
				return &memory{id: "px_gone", at: time.Now()}, relayList(relayAt(relayURL, "t"))
			},
		},
		{
			// Store.Proxies leaves disabled rows out, so a switched-off relay reaches the
			// ladder as one that is simply not in the list.
			"the relay it names has been switched off",
			func(_ *testing.T, _ string) (*memory, Relays) {
				return &memory{id: "px_1", at: time.Now()}, relayList()
			},
		},
		{
			"it will not read",
			func(_ *testing.T, relayURL string) (*memory, Relays) {
				return &memory{failRead: true}, relayList(relayAt(relayURL, "t"))
			},
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			var asked int
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked++
				w.Header().Set("Content-Type", "application/rss+xml")
				w.Write([]byte(feedXML))
			}))
			defer origin.Close()

			proxy, seen := proxio(t, "t")
			routes, relays := tc.build(t, proxy.URL)

			f := NewFetcher("http://read.example.com")
			f.Proxies, f.Routes = relays, routes

			if _, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now()); err != nil {
				t.Fatalf("Fetch(): %v", err)
			}
			// The point: the direct request is what answered, so the ladder started at the
			// top rather than at whatever was remembered.
			if asked != 1 {
				t.Errorf("the publisher was asked %d times, want 1", asked)
			}
			if len(*seen) != 0 {
				t.Errorf("a relay was used though the route should have been ignored: %v", *seen)
			}
		})
	}
}

// A relay that has stopped working costs one wasted request and then stops being tried first.
func TestARouteThatStopsWorkingIsDropped(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	defer origin.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	routes := &memory{id: "px_1", at: time.Now()}
	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(relayAt(deadURL, "t"))
	f.Routes = routes

	if _, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now()); err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if routes.forgets == 0 || routes.id != "" {
		t.Errorf("a route through a relay that is down was kept: %+v", routes)
	}
}

// A route is remembered per publisher, not per host, so its pictures are relayed too.
func TestARouteCoversAPublishersOtherHosts(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://www.theguardian.com/world/rss", "theguardian.com"},
		{"https://i.guim.co.uk/img/media/x.jpg", "guim.co.uk"},
		{"https://feeds.example.co.uk/rss", "example.co.uk"},
		{"http://192.168.1.10:8080/feed", "192.168.1.10"},
		{"http://localhost:9000/feed", "localhost"},
	} {
		u, err := url.Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := domainOf(u); got != tc.want {
			t.Errorf("domainOf(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// markingAny tags requests going to any of several relays.
type markingAny struct {
	inner  http.RoundTripper
	relays []string
}

func (m markingAny) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, relay := range m.relays {
		if strings.HasPrefix(req.URL.String(), relay) {
			req = req.Clone(req.Context())
			req.Header.Set("X-Pretend-Relayed", "1")
			break
		}
	}
	return m.inner.RoundTrip(req)
}

// A route is permanent, because what it records does not lapse.
//
// A publisher restricted by region is restricted by region — that is its licensing, not a state
// that lifts — so nothing re-probes the direct request on a timer. Spending one failed request
// per blocked domain per period to re-learn something that changes on the order of never is the
// cost this does not pay.
func TestARouteDoesNotLapse(t *testing.T) {
	var direct int
	origin := blocked(t, &direct)
	proxy, seen := proxio(t, "t")

	// Learned a year ago, which under an expiring route would be long past rechecking.
	routes := &memory{id: "px_1", at: time.Now().Add(-365 * 24 * time.Hour)}
	f := NewFetcher("http://read.example.com")
	f.Proxies, f.Routes = relayList(relayAt(proxy.URL, "t")), routes
	f.client.Transport = marking{http.DefaultTransport, proxy.URL}

	if _, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now()); err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if direct != 0 {
		t.Errorf("the publisher was asked directly %d times; the route should have gone first", direct)
	}
	if len(*seen) != 1 {
		t.Errorf("the relay handled %d requests, want 1", len(*seen))
	}
}

// What ends a route is the relay failing, and the direct request is the very next thing tried.
//
// This is the whole recovery story now that nothing expires: a block that has lifted is noticed
// the first time the relay has a bad moment, and the route stays gone because the direct
// request succeeded.
func TestARelayFailingIsWhatNoticesABlockHasLifted(t *testing.T) {
	var direct int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The block has lifted: this now answers anybody.
		direct++
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	defer origin.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	routes := &memory{id: "px_1", at: time.Now()}
	f := NewFetcher("http://read.example.com")
	f.Proxies, f.Routes = relayList(relayAt(deadURL, "t")), routes

	if _, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now()); err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if direct != 1 {
		t.Errorf("the publisher was asked directly %d times, want 1", direct)
	}
	if routes.id != "" || routes.forgets == 0 {
		t.Errorf("the route survived the relay failing and the direct request working: %+v", routes)
	}
}

// A rate limit does not become a permanent route.
//
// The one refusal that is about the moment rather than about who is asking — and the one that
// would otherwise mint a permanent route out of it, because a rate limit is counted per caller
// so the relay sails through. Every other refusal that gets as far as creating a route is
// structural by construction: a transient fault fails through the relay too, so nothing is
// written down.
func TestARateLimitIsNotLearnedAsARoute(t *testing.T) {
	for _, tc := range []struct {
		status   int
		remember bool
	}{
		{http.StatusTooManyRequests, false},
		{http.StatusForbidden, true},
		{http.StatusUnavailableForLegalReasons, true},
	} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Pretend-Relayed") == "" {
				w.WriteHeader(tc.status)
				return
			}
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write([]byte(feedXML))
		}))
		proxy, seen := proxio(t, "t")

		routes := &memory{}
		f := NewFetcher("http://read.example.com")
		f.Proxies, f.Routes = relayList(relayAt(proxy.URL, "t")), routes
		f.client.Transport = marking{http.DefaultTransport, proxy.URL}

		got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
		origin.Close()
		if err != nil {
			t.Fatalf("%d: Fetch(): %v", tc.status, err)
		}

		// Either way the article arrives — this is about what is written down, not about
		// whether the fetch works.
		if got.Parsed == nil {
			t.Errorf("%d: the relay did not bring back the feed", tc.status)
		}
		if len(*seen) != 1 {
			t.Errorf("%d: the relay handled %d requests, want 1", tc.status, len(*seen))
		}
		if remembered := routes.writes > 0; remembered != tc.remember {
			t.Errorf("%d: remembered = %v, want %v", tc.status, remembered, tc.remember)
		}
	}
}

// transient is about the moment, and everything else is about who is asking.
func TestWhichRefusalsAreAboutTheMoment(t *testing.T) {
	for _, tc := range []struct {
		what   string
		status int
		err    error
		want   bool
	}{
		{"a rate limit", http.StatusTooManyRequests, nil, true},
		{"a geo-block", http.StatusForbidden, nil, false},
		{"a legal block", http.StatusUnavailableForLegalReasons, nil, false},
		{"nothing was reached", 0, context.DeadlineExceeded, false},
		{"it worked", http.StatusOK, nil, false},
	} {
		if got := transient(tc.status, tc.err); got != tc.want {
			t.Errorf("%s: transient = %v, want %v", tc.what, got, tc.want)
		}
	}
}

// A route through a relay that is down is dropped even when nothing else works either.
//
// The case the direct-success path does not cover, and the reason dropping happens where the
// relay fails rather than only where the direct request succeeds: with the publisher still
// blocking and the relay still down, nothing else would clear the route, and every fetch would
// spend a request on the dead relay before starting the ladder it was always going to run.
func TestARouteToADeadRelayIsDroppedEvenWhenNothingElseWorks(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer origin.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	routes := &memory{id: "px_1", at: time.Now()}
	f := NewFetcher("http://read.example.com")
	f.Proxies, f.Routes = relayList(relayAt(deadURL, "t")), routes

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded though nothing got through")
	}
	if got == nil || got.Status != http.StatusForbidden {
		t.Fatalf("reported %+v, want the publisher's own 403", got)
	}
	if routes.id != "" || routes.forgets == 0 {
		t.Errorf("a route to a dead relay survived a fetch where nothing worked: %+v", routes)
	}
}
