package feeds

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// TestAgainstARealProxio runs the ladder against an actual proxio container.
//
// Skipped unless PROXIO_URL and PROXIO_TOKEN are set, so it is a thing to reach for rather than
// part of the ordinary suite — but it checks the one class of thing the stand-in cannot. The
// stand-in implements what this program *believes* proxio's interface is: that the parameters
// are named `url` and `token`, that `hide` is spelled that way, and that a refusal carries
// X-Proxio-Error. If any of that is wrong, every unit test still passes and nothing works.
//
//	docker run -d -p 18080:80 -v <data>:/data ghcr.io/reeywhaar/proxio:latest serve
//	PROXIO_URL=http://localhost:18080 PROXIO_TOKEN=<token> \
//	  PROXIO_HOST=$(ipconfig getifaddr en0) \
//	  go test ./internal/feeds/ -run RealProxio -v
//
// PROXIO_HOST is the address the *container* can reach this machine on, because the stand-in
// publisher runs here and the relay has to fetch it from over there. One URL has to work from
// both sides, so it is bound to every interface and named by that address — 127.0.0.1 would be
// the container itself, and host.docker.internal does not resolve out here.
func TestAgainstARealProxio(t *testing.T) {
	relayURL, token := os.Getenv("PROXIO_URL"), os.Getenv("PROXIO_TOKEN")
	if relayURL == "" || token == "" {
		t.Skip("set PROXIO_URL and PROXIO_TOKEN to run this against a real container")
	}

	host := os.Getenv("PROXIO_HOST")
	if host == "" {
		t.Skip("set PROXIO_HOST to an address the container can reach this machine on")
	}

	var direct int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// proxio inherits the caller's headers, so a marker put on the request to the relay
		// arrives here on the request the relay makes. Its absence is what a direct request
		// looks like.
		//
		// Not the same trick the SOCKS test uses, and the difference is the point: proxio
		// sends over this same client, so marking everything that leaves it would mark the
		// relayed request too.
		if r.Header.Get("X-Pretend-Relayed") == "" {
			direct++
			w.WriteHeader(http.StatusForbidden)
			return
		}
		t.Logf("relayed request arrived: user-agent=%q if-none-match=%q x-forwarded-for=%q",
			r.Header.Get("User-Agent"), r.Header.Get("If-None-Match"),
			r.Header.Get("X-Forwarded-For"))
		w.Header().Set("ETag", `W/"real"`)
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	})

	// Bound to every interface rather than to the loopback httptest picks, so the one URL
	// below is reachable from here and from inside the container.
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	origin.Start()
	defer origin.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + net.JoinHostPort(host, port)
	t.Logf("the publisher is at %s, and the relay at %s", target, relayURL)

	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(&store.Proxy{ID: "px_1", Kind: store.ProxioKind,
		Label: "real proxio", URL: relayURL, Token: token, Enabled: true})
	f.client.Transport = marking{http.DefaultTransport, relayURL}

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: target}, time.Now())
	if err != nil {
		t.Fatalf("Fetch() through a real proxio: %v", err)
	}
	if got.Parsed == nil || got.Parsed.Title != "Blocked Daily" {
		t.Fatalf("the real relay did not bring back the feed: %+v", got)
	}
	if direct != 1 {
		t.Errorf("asked directly %d times, want one refusal before the relay", direct)
	}
	// The validators survive the round trip, which is what makes a 304 possible through one.
	if got.ETag != `W/"real"` {
		t.Errorf("ETag = %q; the relay did not hand back the target's headers", got.ETag)
	}
	// And the address reported is the publisher's, with no token in it.
	if got.FinalURL != target {
		t.Errorf("FinalURL = %q, want %q", got.FinalURL, target)
	}
	if strings.Contains(got.FinalURL, token) {
		t.Fatalf("the token reached FinalURL: %q", got.FinalURL)
	}

	// A wrong token has to be the relay's fault, told apart by X-Proxio-Error — the whole
	// failover depends on that header existing and being spelled this way.
	bad := &store.Proxy{ID: "px_2", Kind: store.ProxioKind, Label: "wrong token",
		URL: relayURL, Token: "definitely-not-a-real-token", Enabled: true}
	res, err := once(context.Background(), f.client,
		must(http.NewRequest(http.MethodGet, target, nil)), bad)
	if err != nil {
		t.Fatalf("dialling with a bad token: %v", err)
	}
	defer res.Body.Close()
	t.Logf("a bad token answered %s with %s=%q", res.Status, proxyError, faulted(res))
	if faulted(res) == "" {
		t.Errorf("a refused token carried no %s header; a relay's own failure would be "+
			"reported as the publisher's", proxyError)
	}
}
