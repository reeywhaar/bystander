package api

import (
	"net/http"
	"testing"

	"bystander/internal/store"
)

// A page you are reading does not lose its sources when you unfollow a feed.
//
// The reported bug, and it took three things: the titles on a page come from the reader's
// subscriptions, so unfollowing emptied the label and the card said it came from nowhere; the
// sweep then collected the feed row itself, so there was nothing left to fall back to; and
// unfollowing forgot what had been read there, so a page somebody had worked through came back
// as a page of unread cards.
func TestUnfollowingAFeedLeavesThePageIntact(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 6)
	h.signIn(store.RoleUser, "alice")

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}),
		http.StatusCreated, &sub)

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) == 0 {
		t.Fatal("nothing was composed to unfollow from")
	}
	title := page.Items[0].Feed.Title
	if title == "" {
		t.Fatal("the feed had no name to begin with")
	}

	// Read one, so the page has state worth keeping.
	read := page.Items[0].ID
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+read+"/read", nil), http.StatusNoContent, nil)

	h.expect(h.do(http.MethodDelete, "/api/feeds/"+sub.ID, nil), http.StatusNoContent, nil)

	// The sweep's orphan collection, which is where the feed row used to go.
	onPages, err := h.store.FeedIDsOnLivePages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DeleteOrphanFeeds(t.Context(), onPages); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.FeedByID(t.Context(), sub.FeedID); err != nil {
		t.Fatalf("the feed was collected while its articles were on a live page: %v", err)
	}

	var after editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &after)
	if len(after.Items) != len(page.Items) {
		t.Fatalf("the page went from %d articles to %d", len(page.Items), len(after.Items))
	}

	stillRead := false
	for _, item := range after.Items {
		if item.Feed.Title != title {
			t.Errorf("%q now says it came from %q, want %q", item.Title, item.Feed.Title, title)
		}
		// No subscription any more, so nothing for the interface to hang a control off.
		if item.Feed.SubscriptionID != "" {
			t.Errorf("%q still carries a subscription id after unfollowing", item.Title)
		}
		if item.ID == read && item.ReadAt != nil {
			stillRead = true
		}
	}
	if !stillRead {
		t.Error("an article that had been read came back as unread")
	}
}
