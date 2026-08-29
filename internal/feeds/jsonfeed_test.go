package feeds

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// One JSON Feed, with the fields that have somewhere to go.
const jsonFeedBody = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "The Example",
  "home_page_url": "https://example.com/",
  "feed_url": "https://example.com/feed.json",
  "items": [
    {
      "id": "1",
      "url": "https://example.com/one",
      "title": "A story about a thing",
      "content_html": "<p>What the thing was.</p><script>bad()</script>",
      "image": "https://example.com/pic.png",
      "date_published": "2026-08-20T10:00:00Z"
    }
  ]
}`

// JSON Feed is read by the same parser as RSS and Atom, which is the reason gofeed is a
// dependency at all — and the reason nothing here says which format it is holding.
//
// Tested because it is the one of the three nothing else exercises: every other test in this
// package speaks XML, so JSON support rests entirely on a dependency continuing to do
// something no test of ours would notice it stopping. It is claimed on the landing page and
// in the README, which is the other half of why it is pinned here.
func TestAJSONFeedIsReadLikeAnyOther(t *testing.T) {
	parsed, err := Parse(strings.NewReader(jsonFeedBody),
		"https://example.com/feed.json", "f_1", time.Now())
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}

	if parsed.Title != "The Example" {
		t.Errorf("title = %q, want The Example", parsed.Title)
	}
	if parsed.SiteURL != "https://example.com/" {
		t.Errorf("site = %q, want https://example.com/", parsed.SiteURL)
	}
	if len(parsed.Items) != 1 {
		t.Fatalf("%d items, want 1", len(parsed.Items))
	}

	item := parsed.Items[0]
	if item.Title != "A story about a thing" {
		t.Errorf("title = %q", item.Title)
	}
	if item.Link != "https://example.com/one" {
		t.Errorf("link = %q", item.Link)
	}
	// JSON Feed's own `image`, which is the format's answer to an enclosure.
	if item.ImageURL != "https://example.com/pic.png" {
		t.Errorf("image = %q", item.ImageURL)
	}
	if want := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC); !item.PublishedAt.Equal(want) {
		t.Errorf("published = %s, want %s", item.PublishedAt, want)
	}
	// Sanitized on the way in, once, like every other format's summary — the allowlist knows
	// nothing about where the HTML came from, and this proves JSON is not a way round it.
	if strings.Contains(item.Summary, "<script") {
		t.Errorf("the script survived sanitizing: %q", item.Summary)
	}
	if !strings.Contains(item.Summary, "What the thing was.") {
		t.Errorf("summary = %q, want the prose kept", item.Summary)
	}
}

// The format is worked out from what came back, never from what the address ends in.
//
// Discover parses the body before it looks for a single <link> tag, so a JSON feed served at
// an address that says "rss" is read as what it is. That is the whole of the claim on the
// landing page — "told apart by reading them rather than by what the address ends in".
func TestAJSONFeedIsFoundAtAnAddressThatLies(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/feed+json")
		w.Write([]byte(jsonFeedBody))
	}))
	defer host.Close()

	found, err := NewFetcher("http://read.example.com").Discover(
		t.Context(), host.URL+"/rss.xml", time.Now())
	if err != nil {
		t.Fatalf("Discover(): %v", err)
	}
	if found.Feed == nil {
		t.Fatal("the address was taken for a page rather than read as a feed")
	}
	if found.Feed.Title != "The Example" {
		t.Errorf("title = %q, want The Example", found.Feed.Title)
	}
}

// A site that declares nothing and publishes only JSON is the one case guessing has to cover.
func TestASiteDeclaringNothingIsGuessedAtForJSON(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.json" {
			w.Header().Set("Content-Type", "application/feed+json")
			w.Write([]byte(jsonFeedBody))
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Nothing declared</title></head><body></body></html>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer host.Close()

	found, err := NewFetcher("http://read.example.com").Discover(t.Context(), host.URL, time.Now())
	if err != nil {
		t.Fatalf("Discover(): %v", err)
	}
	if len(found.Candidates) != 1 {
		t.Fatalf("%d candidates, want 1", len(found.Candidates))
	}
	if !strings.HasSuffix(found.Candidates[0].URL, "/feed.json") {
		t.Errorf("guessed %q, want the JSON feed", found.Candidates[0].URL)
	}
}

// The type a site may name for a feed, and the one it may not.
//
// "application/json" is every API endpoint on the web. Accepting it would fill the picker
// with things that are not feeds, on sites that never claimed they were.
func TestOnlyJSONFeedsOwnTypeCountsAsAFeedLink(t *testing.T) {
	page := func(ctype string) string {
		return `<html><head><link rel="alternate" type="` + ctype +
			`" href="/feed.json"></head><body></body></html>`
	}

	for _, tc := range []struct {
		ctype string
		want  bool
	}{
		{"application/feed+json", true},
		{"application/json", false},
	} {
		links := feedLinks(page(tc.ctype), "https://example.com/")
		if got := len(links) > 0; got != tc.want {
			t.Errorf("%s counted as a feed = %v, want %v", tc.ctype, got, tc.want)
		}
	}
}
