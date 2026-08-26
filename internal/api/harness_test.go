package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"bystander/internal/config"
	"bystander/internal/edition"
	"bystander/internal/feeds"
	mailer "bystander/internal/mail"
	"bystander/internal/session"
	"bystander/internal/store"
)

// harnessPassword is what every account in these tests is created with.
const harnessPassword = "correct-horse"

// harness is a running bystander with a real store on disk, a real router, and a cookie
// jar — so a test walks the same path a browser does rather than calling handlers
// directly.
type harness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	store  *store.Store
	cfg    *config.Config
	api    *Server
	// sessions is here for the one thing tests need from it that requests cannot do:
	// move its clock, so a throttle measured in minutes can be crossed in milliseconds.
	sessions *session.Table
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	t.Setenv(config.PublicURLEnv, "http://read.example.com")
	t.Setenv(config.DataDirEnv, t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// Discard, not stderr: these tests exercise refusals, and a wall of expected error
	// lines makes a real failure harder to find.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// A synthetic bundle rather than whatever web/dist happens to contain, so the shell
	// routing can be asserted precisely.
	dist := fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte(`<div id="root">reader</div>`)},
		"login.html":           &fstest.MapFile{Data: []byte(`<div id="root">login</div>`)},
		"manage.html":          &fstest.MapFile{Data: []byte(`<div id="root">manage</div>`)},
		"admin.html":           &fstest.MapFile{Data: []byte(`<div id="root">admin</div>`)},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte(`console.log("app")`)},
	}
	spa, err := NewSPA(dist, log)
	if err != nil {
		t.Fatalf("NewSPA(): %v", err)
	}

	sessions := session.New(st, cfg.Secure, log)
	fetcher := feeds.NewFetcher(cfg.PublicURL.String())
	generator := edition.NewGenerator(st, log)

	api := New(cfg, st, sessions, generator, fetcher, spa, log)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New(): %v", err)
	}

	return &harness{
		t:        t,
		server:   server,
		client:   &http.Client{Jar: jar, Timeout: 10 * time.Second},
		store:    st,
		cfg:      cfg,
		api:      api,
		sessions: sessions,
	}
}

// sentMail is every message that would have gone out.
type sentMail struct {
	// refuse, when set, is what the relay says instead of accepting.
	refuse error

	mu   sync.Mutex
	sent []mailer.Message
}

func (s *sentMail) record(_ context.Context, _ mailer.Settings, m mailer.Message) error {
	if s.refuse != nil {
		return s.refuse
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return nil
}

// lastTo is the last message sent to an address.
func (s *sentMail) lastTo(t *testing.T, address string) mailer.Message {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.sent) - 1; i >= 0; i-- {
		if strings.EqualFold(s.sent[i].To, address) {
			return s.sent[i]
		}
	}
	t.Fatalf("nothing was sent to %s", address)
	return mailer.Message{}
}

// count is how many messages went out at all, for asserting that none did.
func (s *sentMail) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// codeSentTo digs the confirmation code out of the last message sent to an address.
//
// Read from the message rather than from the database, because the database only has its
// hash — which is the point of storing it that way, and means the only way to know the code
// is to be the recipient.
func (s *sentMail) codeSentTo(t *testing.T, address string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.sent) - 1; i >= 0; i-- {
		if !strings.EqualFold(s.sent[i].To, address) {
			continue
		}
		code := recoveryCode.FindStringSubmatch(s.sent[i].Body)
		if code == nil {
			t.Fatalf("no code in the message sent to %s:\n%s", address, s.sent[i].Body)
		}
		return code[1]
	}
	t.Fatalf("nothing was sent to %s", address)
	return ""
}

// The code as the message writes it: eight of Crockford's base32.
var recoveryCode = regexp.MustCompile(`code is ([0-9A-HJKMNP-TV-Z]{8})`)

// relay configures a relay and captures what would be sent through it.
//
// Both halves are needed together: the handlers refuse to start anything when no relay is
// configured, which is deliberate, so a test about codes has to set one up first.
func (h *harness) relay() *sentMail {
	h.t.Helper()

	if err := h.store.SetSMTP(h.t.Context(), mailer.Settings{
		Host: "smtp.example.com", Port: 587, TLS: mailer.StartTLS,
		Username: "operator", Password: "hunter2", FromAddress: "paper@example.com",
	}); err != nil {
		h.t.Fatalf("SetSMTP(): %v", err)
	}

	out := &sentMail{}
	h.api.sendMail = out.record
	return out
}

// do sends a request. A nil body sends none; anything else is sent as JSON, which is what
// the CSRF guard requires of a mutating request.
func (h *harness) do(method, path string, body any) *http.Response {
	h.t.Helper()
	return h.doAs(h.client, method, path, body)
}

// doAs is do, through somebody else's cookie jar.
func (h *harness) doAs(client *http.Client, method, path string, body any) *http.Response {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal request: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}

	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

// expect asserts a status and decodes the body into out, if out is not nil.
func (h *harness) expect(res *http.Response, status int, out any) {
	h.t.Helper()
	defer res.Body.Close()

	if res.StatusCode != status {
		raw, _ := io.ReadAll(res.Body)
		h.t.Fatalf("%s %s answered %d, want %d: %s",
			res.Request.Method, res.Request.URL.Path, res.StatusCode, status, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatalf("decode response: %v", err)
		}
	}
}

// signIn mints an invitation the way `bystander serve` does on an empty database, accepts
// it, and leaves the client holding a session.
func (h *harness) signIn(role store.Role, username string) {
	h.t.Helper()

	_, token, err := h.store.CreateInvite(h.t.Context(), role, "", "")
	if err != nil {
		h.t.Fatalf("CreateInvite(): %v", err)
	}
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": username, "password": harnessPassword}), http.StatusNoContent, nil)
}

// signInElsewhere signs the same account in again with a jar of its own, as if from another
// device, and hands back the client holding that second session.
//
// A second jar rather than a second harness: the point is two live sessions against one
// account, which is what "this signs out my other devices" is about.
func (h *harness) signInElsewhere(username, password string) *http.Client {
	h.t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("cookiejar.New(): %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	res := h.doAs(client, http.MethodPost, "/api/login",
		map[string]string{"username": username, "password": password})
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		h.t.Fatalf("second sign-in answered %d", res.StatusCode)
	}
	return client
}

// mainPage is the signed-in account's main page id, for the tests that address a page.
// me is the signed-in account's id, for the tests that seed through the store rather than
// through the API.
func (h *harness) me(t *testing.T) string {
	t.Helper()
	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	p, err := h.store.PrincipalByUsername(t.Context(), body.Username)
	if err != nil {
		t.Fatalf("PrincipalByUsername(): %v", err)
	}
	return p.ID
}

func (h *harness) mainPage() string {
	h.t.Helper()
	var pages []pageBody
	h.expect(h.do(http.MethodGet, "/api/pages", nil), http.StatusOK, &pages)
	for _, page := range pages {
		if page.IsMain {
			return page.ID
		}
	}
	h.t.Fatal("this account has no main page")
	return ""
}

// signInAsSomebodyElse creates a second account and hands back a client holding its session.
//
// A different person, not a second device — for the tests that are about one account not being
// able to reach another's things.
func (h *harness) signInAsSomebodyElse(username string) *http.Client {
	h.t.Helper()

	_, token, err := h.store.CreateInvite(h.t.Context(), store.RoleUser, "", "")
	if err != nil {
		h.t.Fatalf("CreateInvite(): %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("cookiejar.New(): %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	res := h.doAs(client, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": username, "password": harnessPassword})
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		h.t.Fatalf("sign-in as %s answered %d", username, res.StatusCode)
	}
	return client
}

// feedServer serves a small RSS feed, and counts how often it was asked for.
type feedServer struct {
	*httptest.Server
	hits int
}

func newFeedServer(t *testing.T, items int) *feedServer {
	t.Helper()

	fs := &feedServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.hits++
		w.Header().Set("Content-Type", "application/rss+xml")

		io.WriteString(w, rssBody(items))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func rssBody(items int) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel>`)
	body.WriteString(`<title>The Example</title><link>https://example.com</link>`)
	for i := range items {
		body.WriteString(`<item>`)
		body.WriteString(`<title>Story ` + itoa(i) + `</title>`)
		body.WriteString(`<link>https://example.com/story-` + itoa(i) + `</link>`)
		body.WriteString(`<guid>https://example.com/story-` + itoa(i) + `</guid>`)
		body.WriteString(`<description><![CDATA[<p>A summary of story ` + itoa(i) +
			`</p><script>alert(1)</script><img src="/pic-` + itoa(i) + `.png">]]></description>`)
		// Published within the last day or so, relative to whenever the test runs. Fixed
		// dates in the past used to work and stopped the moment articles gained an age
		// limit — a feed whose newest article is three weeks old is correctly invisible
		// on a page set to a week.
		body.WriteString(`<pubDate>` +
			time.Now().Add(-time.Duration(i+1)*time.Hour).UTC().Format(time.RFC1123) +
			`</pubDate>`)
		body.WriteString(`</item>`)
	}
	body.WriteString(`</channel></rss>`)
	return body.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// newSiteWithFeeds serves a home page declaring several feeds, the way a real site does.
func newSiteWithFeeds(t *testing.T, feeds map[string]string) *httptest.Server {
	t.Helper()

	var links strings.Builder
	for title, href := range feeds {
		links.WriteString(`<link rel="alternate" type="application/rss+xml" title="` +
			title + `" href="` + href + `">`)
	}
	return newPlainPage(t, `<!doctype html><html><head><title>A Site</title>`+
		links.String()+`</head><body>a site</body></html>`)
}

// newSilentSite is a site that declares no feed and serves one anyway, at a conventional
// address — the shape of every client-rendered site, Reddit included.
func newSilentSite(t *testing.T, feedPath string, items int) *httptest.Server {
	t.Helper()

	fs := &feedServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == feedPath {
			fs.hits++
			w.Header().Set("Content-Type", "application/rss+xml")
			io.WriteString(w, rssBody(items))
			return
		}
		// Everything else is the script shell, with not a <link rel="alternate"> in it.
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><html><head><title>A Site</title></head><body><div id="app"></div></body></html>`)
	}))
	t.Cleanup(fs.Close)
	return fs.Server
}

// newPlainPage serves one HTML document, for the discovery cases.
func newPlainPage(t *testing.T, document string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, document)
	}))
	t.Cleanup(server.Close)
	return server
}
