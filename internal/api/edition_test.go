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

// Saving from one page puts the article on a page of its own, made by the first save, and every
// card says whether the person looking has saved it.
func TestSavingAnArticlePutsItOnTheSavedPage(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 6)
	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}),
		http.StatusCreated, nil)

	var front editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &front)
	if len(front.Items) < 2 {
		t.Fatal("not enough on the page to save one and leave one")
	}
	kept := front.Items[0]
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+kept.ID+"/saved", nil), http.StatusNoContent, nil)

	var pages []pageBody
	h.expect(h.do(http.MethodGet, "/api/pages", nil), http.StatusOK, &pages)
	var saved *pageBody
	for i := range pages {
		if pages[i].IsSaved {
			saved = &pages[i]
		}
	}
	if saved == nil {
		t.Fatalf("no page of saved articles among %+v", pages)
	}

	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &front)
	for _, item := range front.Items {
		if (item.SavedAt != nil) != (item.ID == kept.ID) {
			t.Errorf("%q: saved_at %v, want it set on the saved article alone", item.Title, item.SavedAt)
		}
	}

	var later editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate?page="+saved.Slug, nil), http.StatusOK, &later)
	if len(later.Items) != 1 || later.Items[0].ID != kept.ID {
		t.Fatalf("the saved page holds %+v, want only %q", later.Items, kept.Title)
	}
	if later.Items[0].SavedAt == nil || later.Items[0].Feed.Title == "" {
		t.Errorf("the saved card is %+v, want it marked saved and named", later.Items[0])
	}

	// Let go of, it stays on the page in front of you and says it is no longer kept.
	h.expect(h.do(http.MethodDelete, "/api/edition/items/"+kept.ID+"/saved", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodGet, "/api/edition?page="+saved.Slug, nil), http.StatusOK, &later)
	if len(later.Items) != 1 || later.Items[0].SavedAt != nil {
		t.Errorf("after unsaving, the page holds %+v, want the card still there and unsaved", later.Items)
	}
}
