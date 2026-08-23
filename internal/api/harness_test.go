package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"bystander/internal/config"
	"bystander/internal/edition"
	"bystander/internal/feeds"
	"bystander/internal/session"
	"bystander/internal/store"
)

// harness is a running bystander with a real store on disk, a real router, and a cookie
// jar — so a test walks the same path a browser does rather than calling handlers
// directly.
type harness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	store  *store.Store
	cfg    *config.Config
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
	fetcher := feeds.NewFetcher(cfg.PublicURL.String(), "test")
	generator := edition.NewGenerator(st, log)

	server := httptest.NewServer(New(cfg, st, sessions, generator, fetcher, spa, log).Handler())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New(): %v", err)
	}

	return &harness{
		t:      t,
		server: server,
		client: &http.Client{Jar: jar, Timeout: 10 * time.Second},
		store:  st,
		cfg:    cfg,
	}
}

// do sends a request. A nil body sends none; anything else is sent as JSON, which is what
// the CSRF guard requires of a mutating request.
func (h *harness) do(method, path string, body any) *http.Response {
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
	res, err := h.client.Do(req)
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

	_, token, err := h.store.CreateInvite(h.t.Context(), role, "")
	if err != nil {
		h.t.Fatalf("CreateInvite(): %v", err)
	}
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": username, "password": "correct-horse"}), http.StatusNoContent, nil)
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
		body.WriteString(`<pubDate>Mon, 0` + itoa(1+i%9) + ` Aug 2026 12:00:00 GMT</pubDate>`)
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
