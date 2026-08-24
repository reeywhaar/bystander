package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bystander/internal/store"
)

// Looking at a feed is not following it.
//
// The gap this fills is that a title and an address say almost nothing: a site offering
// "Posts", "Comments" and "Notes" is three plausible names and one right answer, and finding
// out used to mean following one and then unfollowing it again.
func TestAFeedCanBeReadBeforeItIsFollowed(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 15)
	h.signIn(store.RoleUser, "alice")

	var preview previewBody
	h.expect(h.do(http.MethodPost, "/api/feeds/preview",
		map[string]string{"url": feed.URL}), http.StatusOK, &preview)

	if preview.Title != "The Example" {
		t.Errorf("title = %q, want the feed's own", preview.Title)
	}
	// Capped: enough to tell what a publisher writes about, not their whole year.
	if len(preview.Items) != PreviewItems {
		t.Fatalf("%d items, want %d", len(preview.Items), PreviewItems)
	}

	// Newest first, which is not the order a feed arrives in — publishers disagree about
	// that, and somebody judging a feed wants the most recent thing at the top.
	for i := 1; i < len(preview.Items); i++ {
		if preview.Items[i].PublishedAt > preview.Items[i-1].PublishedAt {
			t.Errorf("item %d is newer than the one above it", i)
			break
		}
	}
	if preview.Items[0].Title != "Story 0" {
		t.Errorf("first item = %q, want the newest", preview.Items[0].Title)
	}

	// The same sanitizing pass every stored summary goes through, so what is looked at here
	// is what would arrive.
	if strings.Contains(preview.Items[0].Summary, "<script") {
		t.Errorf("summary = %q, want the script gone", preview.Items[0].Summary)
	}
	if !strings.Contains(preview.Items[0].Summary, "A summary of story 0") {
		t.Errorf("summary = %q, want the prose kept", preview.Items[0].Summary)
	}

	// The picture too, which for some feeds is the whole article: a comic's summary is an
	// image and nothing else, and the sanitizer drops images — so without this a comics feed
	// previews as a list of titles and dates.
	if preview.Items[0].ImageURL == "" {
		t.Error("no picture on an item that has one; a comic would preview as a bare title")
	}

	// And nothing was kept: a feed somebody looked at and did not want leaves no
	// subscription, and no articles for a page to draw from.
	var following []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &following)
	if len(following) != 0 {
		t.Errorf("%d subscriptions after a look, want none", len(following))
	}
	var page editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &page)
	if len(page.Items) != 0 {
		t.Errorf("%d articles on the page after a look, want none", len(page.Items))
	}
}

// A site's address works too, not only a feed's: both are things somebody has in hand when
// they ask what this is.
func TestAPreviewFindsTheFeedOnAPage(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><head><link rel="alternate" type="application/rss+xml" href="`+
			feed.URL+`"></head><body>a site</body></html>`)
	}))
	defer site.Close()

	h.signIn(store.RoleUser, "alice")

	var preview previewBody
	h.expect(h.do(http.MethodPost, "/api/feeds/preview",
		map[string]string{"url": site.URL}), http.StatusOK, &preview)
	if len(preview.Items) != 4 {
		t.Errorf("%d items, want the feed the page names", len(preview.Items))
	}
}

func TestPreviewingSomethingThatIsNotAFeedSaysSo(t *testing.T) {
	h := newHarness(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><body>nothing here</body></html>`)
	}))
	defer site.Close()

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds/preview",
		map[string]string{"url": site.URL}), http.StatusBadRequest, nil)
}
