package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"bystander/internal/store"
)

// recorder serves a page that declares no feed, 404s everything else, and remembers what it
// was asked with.
//
// The root has to answer for the guessing path to run at all: a page that does not load is
// not a page we go looking behind.
type recorder struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec.mu.Lock()
	rec.requests = append(rec.requests, r.Clone(context.Background()))
	rec.mu.Unlock()

	if r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>A Site</title></head><body>x</body></html>`))
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (rec *recorder) seen() []*http.Request {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]*http.Request(nil), rec.requests...)
}

// Every request that leaves this process says who it is and where to complain.
//
// Not politeness for its own sake: an anonymous or absent User-Agent is what gets a fetcher
// rate-limited or blocked outright, and a publisher who wants it to stop needs somewhere to
// look. This covers the guessing path too, which is the one that asks for several addresses
// a site never advertised.
func TestEveryRequestIdentifiesItself(t *testing.T) {
	rec := &recorder{}
	site := httptest.NewServer(rec)
	defer site.Close()

	f := NewFetcher("https://read.example.com", "a1b2c3d")
	ctx := context.Background()
	now := time.Now()

	// Discovery, which fails and then guesses — several requests.
	_, _ = f.Discover(ctx, site.URL, now)
	// And an ordinary poll.
	_, _ = f.Fetch(ctx, &store.Feed{ID: "f_1", CanonicalURL: site.URL + "/feed.xml"}, now)

	requests := rec.seen()
	if len(requests) < 3 {
		t.Fatalf("only %d requests reached the server; the guessing path did not run", len(requests))
	}

	for _, req := range requests {
		agent := req.Header.Get("User-Agent")
		if agent == "" {
			t.Errorf("%s %s went out with no User-Agent", req.Method, req.URL.Path)
			continue
		}
		if !strings.HasPrefix(agent, "bystander/") {
			t.Errorf("User-Agent = %q, want it to name this program", agent)
		}
		// Both links. The project says what the software is; the instance says who to
		// talk to about this particular one. A publisher looking at their logs wants
		// both, and giving them neither is how a fetcher ends up blocked rather than
		// merely rate-limited.
		if !strings.Contains(agent, ProjectURL) {
			t.Errorf("User-Agent = %q, want it to name the project", agent)
		}
		if !strings.Contains(agent, "https://read.example.com") {
			t.Errorf("User-Agent = %q, want it to carry this instance's address", agent)
		}
		// The build, so a publisher can say which version misbehaved.
		if !strings.Contains(agent, "a1b2c3d") {
			t.Errorf("User-Agent = %q, want it to name the build", agent)
		}
		if req.Header.Get("Accept") == "" {
			t.Errorf("%s %s went out with no Accept", req.Method, req.URL.Path)
		}
	}

	t.Logf("%d requests, all as %q", len(requests), requests[0].Header.Get("User-Agent"))
}

// Conditional requests are what make a poll cheap for the publisher, so they are worth
// asserting rather than assuming.
func TestAPollSendsItsValidators(t *testing.T) {
	rec := &recorder{}
	site := httptest.NewServer(rec)
	defer site.Close()

	f := NewFetcher("https://read.example.com", "a1b2c3d")
	_, _ = f.Fetch(context.Background(), &store.Feed{
		ID:           "f_1",
		CanonicalURL: site.URL + "/feed.xml",
		ETag:         `"abc123"`,
		LastModified: "Mon, 01 Aug 2026 12:00:00 GMT",
	}, time.Now())

	requests := rec.seen()
	if len(requests) != 1 {
		t.Fatalf("%d requests, want 1", len(requests))
	}
	if got := requests[0].Header.Get("If-None-Match"); got != `"abc123"` {
		t.Errorf("If-None-Match = %q", got)
	}
	if got := requests[0].Header.Get("If-Modified-Since"); got != "Mon, 01 Aug 2026 12:00:00 GMT" {
		t.Errorf("If-Modified-Since = %q", got)
	}
}
