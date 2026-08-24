package feeds

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// A feed that stops answering has to be explicable, and the explanation is what the server
// said rather than that it said no. "The server answered 503" is a fact nobody can act on.
func TestARefusalKeepsWhatTheServerSaid(t *testing.T) {
	const said = `{"error":"rate limited","retry_after":3600}`

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("\n  " + said + "\n"))
	}))
	defer host.Close()

	result, err := NewFetcher("http://read.example.com").Fetch(
		t.Context(), &store.Feed{CanonicalURL: host.URL}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded against a server that refused")
	}
	if result == nil {
		t.Fatal("no result, so nothing to explain the failure with")
	}
	if result.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", result.Status, http.StatusTooManyRequests)
	}
	// Trimmed, because the surrounding whitespace is the server's formatting and not part of
	// what it said.
	if result.ErrorBody != said {
		t.Errorf("body = %q, want %q", result.ErrorBody, said)
	}
}

// A server answering an error with a whole HTML page must not put a whole HTML page in the
// database, once per feed, for as long as the feed keeps failing.
func TestARefusalIsKeptOnlyAsFarAsItIsUseful(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(strings.Repeat("x", 64<<10)))
	}))
	defer host.Close()

	result, err := NewFetcher("http://read.example.com").Fetch(
		t.Context(), &store.Feed{CanonicalURL: host.URL}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded against a server that refused")
	}
	if len(result.ErrorBody) > maxErrorBody {
		t.Errorf("kept %d bytes of the refusal, want at most %d", len(result.ErrorBody), maxErrorBody)
	}
	if len(result.ErrorBody) == 0 {
		t.Error("kept none of it, so there is nothing to show")
	}
}

// Nothing answered, so there is nothing to quote — and a zero status is what says so. The
// dialog reads exactly this to tell the two situations apart.
func TestARequestThatNeverArrivesHasNothingToQuote(t *testing.T) {
	// Closed immediately, so the port refuses rather than hangs.
	host := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := host.URL
	host.Close()

	result, err := NewFetcher("http://read.example.com").Fetch(
		t.Context(), &store.Feed{CanonicalURL: url}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded against a server that is not there")
	}
	if result != nil && result.Status != 0 {
		t.Errorf("status = %d, want 0 — nothing answered", result.Status)
	}
	if result != nil && result.ErrorBody != "" {
		t.Errorf("body = %q, want nothing: there was no answer to keep", result.ErrorBody)
	}
}
