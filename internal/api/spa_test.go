package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"bystander/internal/session"
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
		// "/" without a session is the landing page, which lives in the login island. See
		// the pair of tests below — it is the one path here that reads the request.
		{"/", "login"},
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

// "/" is two pages, and which one you get is the only routing decision that reads the request.
//
// To somebody with an account it is their front page. To a stranger it is the only thing this
// instance has to say about what it is — and handing them the reader's shell means a bundle
// they cannot use, a 401, and a whole-document redirect before a word about what they are
// looking at.
func TestTheFrontPageIsTheLandingPageToAStranger(t *testing.T) {
	h := newHarness(t)

	res := h.get("/", "text/html")
	if got := body(t, res); !strings.Contains(got, "login") {
		t.Errorf("a stranger at / got %q, want the landing page's island", got)
	}

	// And a shared cache must not hand that answer to somebody who has an account, or the
	// other way about.
	if vary := res.Header.Get("Vary"); !strings.Contains(vary, "Cookie") {
		t.Errorf("Vary = %q, want it to name Cookie", vary)
	}
}

func TestTheFrontPageIsTheReaderToSomebodySignedIn(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	if got := body(t, h.get("/", "text/html")); !strings.Contains(got, "reader") {
		t.Errorf("a signed-in visitor at / got %q, want the reader", got)
	}
}

// Presence, not validity. Resolving the session here would be a database read and a
// sliding-expiry write inside a GET, to choose which HTML to send — so a stale cookie gets
// the reader, which asks /api/me, is refused, and sends them on. That is the right fallback
// rather than a missed case.
func TestAStaleCookieStillGetsTheReader(t *testing.T) {
	h := newHarness(t)

	req, err := http.NewRequest(http.MethodGet, h.server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: "long-since-expired"})
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res); !strings.Contains(got, "reader") {
		t.Errorf("a stale cookie at / got %q, want the reader to sort it out", got)
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

// An instance can turn the landing page off, and then "/" is what it was before.
//
// On by default, and the only switch on instance_settings that starts that way: the other two
// are exposure, where a default of yes decides something on somebody's behalf. This one only
// decides what the front door says.
func TestAnInstanceCanTurnTheLandingPageOff(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	// Signed in, so this asks for the reader either way — the landing page is for strangers.
	// A second client with no cookie is the visitor under test.
	stranger := &http.Client{}

	ask := func() string {
		req, err := http.NewRequest(http.MethodGet, h.server.URL+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html")
		res, err := stranger.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return body(t, res)
	}

	if got := ask(); !strings.Contains(got, "login") {
		t.Fatalf("a stranger got %q, want the landing page by default", got)
	}

	h.expect(h.do(http.MethodPut, "/api/admin/instance",
		map[string]bool{"public_pages": false, "public_indexing": false, "landing": false}),
		http.StatusOK, nil)

	if got := ask(); !strings.Contains(got, "reader") {
		t.Errorf("with the landing page off a stranger got %q, want the old behaviour", got)
	}

	// And back, because the switch has to work in both directions to be a switch — this is
	// also what proves the cache behind it is invalidated where it is written.
	h.expect(h.do(http.MethodPut, "/api/admin/instance",
		map[string]bool{"public_pages": false, "public_indexing": false, "landing": true}),
		http.StatusOK, nil)

	if got := ask(); !strings.Contains(got, "login") {
		t.Errorf("turned back on, a stranger got %q, want the landing page", got)
	}
}
