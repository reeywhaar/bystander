package api

import (
	"net/http"
	"testing"
	"time"

	"bystander/internal/feeds"
	"bystander/internal/store"
)

// The screen that says an instance is quietly cropping half its comics.
//
// A page draws a shape for a picture it has no measurements for, which looks like a design
// choice rather than a fault — so this used to be invisible. The breakdown is the point: a
// hundred failures that all say "refused" is one host with hotlink protection, and a hundred
// that say "undecodable" is a format this build cannot read.
func TestTheImagesScreenSaysWhyPicturesAreUnmeasured(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	feed, err := h.store.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := h.store.SaveItems(t.Context(), []*store.Item{
		{ID: "a", FeedID: feed.ID, GUID: "1", Title: "One", Link: "https://example.com/1",
			ImageURL: "https://example.com/measured.png", PublishedAt: now, FetchedAt: now},
		{ID: "b", FeedID: feed.ID, GUID: "2", Title: "Two", Link: "https://example.com/2",
			ImageURL: "https://example.com/gone.png", PublishedAt: now, FetchedAt: now},
		{ID: "c", FeedID: feed.ID, GUID: "3", Title: "Three", Link: "https://example.com/3",
			ImageURL: "https://example.com/waiting.png", PublishedAt: now, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetImageSize(t.Context(), "https://example.com/measured.png", 800, 600); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PostponeImage(t.Context(), "https://example.com/gone.png",
		feeds.Gone, feeds.MeasureRetryLater); err != nil {
		t.Fatal(err)
	}

	var body imagesBody
	h.expect(h.do(http.MethodGet, "/api/admin/images", nil), http.StatusOK, &body)

	if body.Pictures != 3 || body.Measured != 1 || body.Unmeasured != 2 {
		t.Errorf("%d pictures, %d measured, %d without a size; want 3, 1, 2",
			body.Pictures, body.Measured, body.Unmeasured)
	}
	// One that failed for a named reason, and one nothing has asked about yet — which is a
	// queue that has not caught up rather than a failure, and reads differently.
	got := map[string]int{}
	for _, failure := range body.Failures {
		got[failure.Reason] = failure.Count
	}
	if got[feeds.Gone] != 1 || got[""] != 1 {
		t.Errorf("failures = %v, want one gone and one not yet asked about", got)
	}

	// Reset by reason touches that reason and nothing else.
	var reset struct {
		Queued int `json:"queued"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/images/retry",
		map[string]string{"reason": feeds.Gone}), http.StatusOK, &reset)
	if reset.Queued != 1 {
		t.Errorf("reset %d pictures, want the one that was gone", reset.Queued)
	}

	// And the measured one is never dragged back in, whatever is pressed.
	h.expect(h.do(http.MethodPost, "/api/admin/images/retry",
		map[string]string{"reason": ""}), http.StatusOK, &reset)
	if reset.Queued != 2 {
		t.Errorf("reset %d pictures, want the two without a size — never the measured one", reset.Queued)
	}
}

func TestTheImagesScreenIsForAdministrators(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "bob")
	h.expect(h.do(http.MethodGet, "/api/admin/images", nil), http.StatusForbidden, nil)
	h.expect(h.do(http.MethodPost, "/api/admin/images/retry",
		map[string]string{"reason": ""}), http.StatusForbidden, nil)
}

// The counts say what is wrong; the list behind one says with what.
//
// Forty pictures under "refused" is either one host with hotlink protection or forty
// publishers each losing one, and nothing but the addresses tells those apart.
func TestOneReasonListsThePicturesBehindIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	feed, err := h.store.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// One picture two articles share, one used once, and one that failed for another reason.
	if _, err := h.store.SaveItems(t.Context(), []*store.Item{
		{ID: "a", FeedID: feed.ID, GUID: "1", Title: "Older", Link: "https://example.com/1",
			ImageURL:    "https://cdn.example.com/shared.png",
			PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
		{ID: "b", FeedID: feed.ID, GUID: "2", Title: "Newer", Link: "https://example.com/2",
			ImageURL: "https://cdn.example.com/shared.png", PublishedAt: now, FetchedAt: now},
		{ID: "c", FeedID: feed.ID, GUID: "3", Title: "Lonely", Link: "https://example.com/3",
			ImageURL: "https://cdn.example.com/one.png", PublishedAt: now, FetchedAt: now},
		{ID: "d", FeedID: feed.ID, GUID: "4", Title: "Elsewhere", Link: "https://example.com/4",
			ImageURL: "https://other.example.com/gone.png", PublishedAt: now, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"https://cdn.example.com/shared.png", "https://cdn.example.com/one.png"} {
		if err := h.store.PostponeImage(t.Context(), url, feeds.Refused, feeds.MeasureRetryLater); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.PostponeImage(t.Context(), "https://other.example.com/gone.png",
		feeds.Gone, feeds.MeasureRetryLater); err != nil {
		t.Fatal(err)
	}

	var body unmeasuredImagesBody
	h.expect(h.do(http.MethodGet, "/api/admin/images/unmeasured?reason="+feeds.Refused, nil),
		http.StatusOK, &body)

	if len(body.Pictures) != 2 {
		t.Fatalf("%d pictures, want the two that were refused: %+v", len(body.Pictures), body.Pictures)
	}
	// Most-used first, so the picture the most articles are waiting on leads.
	first := body.Pictures[0]
	if first.URL != "https://cdn.example.com/shared.png" || first.Articles != 2 {
		t.Errorf("first row is %s used by %d, want the shared one used by 2", first.URL, first.Articles)
	}
	// The newest article using it, because a bare CDN address names no publisher.
	if first.Title != "Newer" {
		t.Errorf("title = %q, want the newest article using the picture", first.Title)
	}
	if first.RetryAt == 0 {
		t.Error("no retry time; the row cannot say when it will be asked about again")
	}
	// The one that failed for another reason is not in this list.
	for _, pic := range body.Pictures {
		if pic.Reason != feeds.Refused {
			t.Errorf("%s says %q in a list of %q", pic.URL, pic.Reason, feeds.Refused)
		}
	}

	// Resetting one address touches that address and leaves the rest of its category alone —
	// which is the whole reason it is separate from resetting the category.
	var reset struct {
		Queued int `json:"queued"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/images/retry",
		map[string]string{"url": "https://cdn.example.com/shared.png"}), http.StatusOK, &reset)
	if reset.Queued != 2 {
		t.Errorf("reset %d articles, want the two sharing that picture", reset.Queued)
	}

	// It stays in the group, and that is right rather than a miss: resetting makes a picture
	// *due*, it does not decide anything about it. The reason it failed last time is still the
	// last thing known about it, and it stops being true when the measurer says so.
	h.expect(h.do(http.MethodGet, "/api/admin/images/unmeasured?reason="+feeds.Refused, nil),
		http.StatusOK, &body)
	if len(body.Pictures) != 2 {
		t.Fatalf("%d pictures after a reset, want both still listed: %+v", len(body.Pictures), body.Pictures)
	}
	for _, pic := range body.Pictures {
		due := pic.RetryAt == 0
		if want := pic.URL == "https://cdn.example.com/shared.png"; due != want {
			t.Errorf("%s is due=%v, want %v — a reset moves one address and leaves the rest waiting",
				pic.URL, due, want)
		}
	}
}

// Pictures nothing has asked about yet are a real group, and an empty path segment is not a
// path — which is why the reason is a query parameter.
func TestTheWaitingGroupCanBeListed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	feed, err := h.store.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := h.store.SaveItems(t.Context(), []*store.Item{
		{ID: "a", FeedID: feed.ID, GUID: "1", Title: "Waiting", Link: "https://example.com/1",
			ImageURL: "https://example.com/new.png", PublishedAt: now, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	var body unmeasuredImagesBody
	h.expect(h.do(http.MethodGet, "/api/admin/images/unmeasured?reason=", nil), http.StatusOK, &body)
	if len(body.Pictures) != 1 || body.Pictures[0].RetryAt != 0 {
		t.Errorf("waiting pictures = %+v, want the one, already due", body.Pictures)
	}
}

func TestListingPicturesIsForAdministrators(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "bob")
	h.expect(h.do(http.MethodGet, "/api/admin/images/unmeasured?reason=gone", nil),
		http.StatusForbidden, nil)
}
