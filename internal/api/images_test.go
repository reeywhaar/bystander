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
