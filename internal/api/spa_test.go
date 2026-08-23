package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"bystander/internal/store"
)

// get fetches a path without following redirects and without a JSON body, the way a
// browser navigating would.
func (h *harness) get(path string, accept string) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}

// Which island a navigation lands on is the whole of the routing between them.
func TestShellRouting(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct{ path, want string }{
		{"/", "reader"},
		{"/anything-else", "reader"},
		{"/login", "login"},
		{"/invite/abc123", "login"},
		// A truncated link — messaging apps cut long URLs — belongs to the island that
		// can say "this link looks incomplete", not to the reader's shell.
		{"/invite", "login"},
		{"/manage", "manage"},
		{"/manage/feeds", "manage"},
		{"/admin", "admin"},
		{"/admin/users", "admin"},
	} {
		res := h.get(tc.path, "text/html")
		got := body(t, res)
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s answered %d", tc.path, res.StatusCode)
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("GET %s served %q, want the %s shell", tc.path, got, tc.want)
		}
	}
}

// A missing script must be a 404 in devtools, not an HTML document served with a
// JavaScript content type — that failure presents as a MIME error with no hint that the
// file simply is not there.
func TestMissingAssetsAreNotTheShell(t *testing.T) {
	h := newHarness(t)

	res := h.get("/assets/gone-abc.js", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("a missing asset answered %d, want 404", res.StatusCode)
	}
}

func TestAssetsAreCachedHard(t *testing.T) {
	h := newHarness(t)

	res := h.get("/assets/app-abc123.js", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the asset answered %d", res.StatusCode)
	}
	// Vite content-hashes these names, so the bytes at a given URL never change.
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q", ct)
	}

	// A shell must never be cached hard, or a deploy strands browsers on a stale bundle
	// reference.
	shell := h.get("/", "text/html")
	defer shell.Body.Close()
	if cc := shell.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("the shell's Cache-Control = %q, want no-cache", cc)
	}
}

func TestETagsAreHonoured(t *testing.T) {
	h := newHarness(t)

	first := h.get("/assets/app-abc123.js", "")
	first.Body.Close()
	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag was sent")
	}

	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/assets/app-abc123.js", nil)
	req.Header.Set("If-None-Match", etag)
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotModified {
		t.Errorf("a matching ETag answered %d, want 304", res.StatusCode)
	}
}

// A mistyped API path must come back as JSON. Falling through to the SPA would hand a
// fetch an HTML document and present as a parse error with no hint of the real problem.
func TestUnknownApiPathsStayJSON(t *testing.T) {
	h := newHarness(t)

	res := h.get("/api/nope", "text/html")
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /api/nope answered %d, want 404", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
}

// The three-part guard. SameSite=Lax is the first part and is asserted on the cookie
// itself; these are the other two.
func TestCsrfGuard(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	// A cross-site mutating request, whatever it carries.
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/edition/regenerate", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("cross-site POST: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site POST answered %d, want 403", res.StatusCode)
	}

	// A form post — the shape most cross-site attempts take — cannot declare JSON.
	form := strings.NewReader("url=https://example.com")
	req, _ = http.NewRequest(http.MethodPost, h.server.URL+"/api/feeds", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err = h.client.Do(req)
	if err != nil {
		t.Fatalf("form POST: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a form post answered %d, want 415", res.StatusCode)
	}

	// A safe method is never refused for either reason.
	req, _ = http.NewRequest(http.MethodGet, h.server.URL+"/api/me", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err = h.client.Do(req)
	if err != nil {
		t.Fatalf("cross-site GET: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("a cross-site GET answered %d, want 200", res.StatusCode)
	}
}

// The absence of CORS is load-bearing: it is what makes two of the three CSRF defences
// worth anything. This test exists so nobody adds a permissive middleware "to fix a
// development problem" without it failing here.
func TestNoCorsHeaderIsEverEmitted(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/nope"},
		{http.MethodOptions, "/api/feeds"},
		{http.MethodPost, "/api/logout"},
	} {
		req, _ := http.NewRequest(tc.method, h.server.URL+tc.path, nil)
		req.Header.Set("Origin", "https://evil.example")
		res, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		res.Body.Close()

		for _, header := range []string{
			"Access-Control-Allow-Origin",
			"Access-Control-Allow-Credentials",
			"Access-Control-Allow-Methods",
		} {
			if v := res.Header.Get(header); v != "" {
				t.Errorf("%s %s emitted %s: %q", tc.method, tc.path, header, v)
			}
		}
	}
}
