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

	// Saved and read where it was found: putting something aside is dealing with it here.
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &front)
	for _, item := range front.Items {
		if (item.SavedAt != nil) != (item.ID == kept.ID) {
			t.Errorf("%q: saved_at %v, want it set on the saved article alone", item.Title, item.SavedAt)
		}
		if (item.ReadAt != nil) != (item.ID == kept.ID) {
			t.Errorf("%q: read_at %v, want saving to have read the saved article alone", item.Title, item.ReadAt)
		}
	}

	// Composed on the first look, without anybody asking for a new page.
	var later editionBody
	h.expect(h.do(http.MethodGet, "/api/edition?page="+saved.Slug, nil), http.StatusOK, &later)
	if len(later.Items) != 1 || later.Items[0].ID != kept.ID {
		t.Fatalf("the saved page holds %+v, want only %q", later.Items, kept.Title)
	}
	if later.Items[0].SavedAt == nil || later.Items[0].Feed.Title == "" {
		t.Errorf("the saved card is %+v, want it marked saved and named", later.Items[0])
	}
	// But not read on the page it was saved to, where it has only just arrived.
	if later.Items[0].ReadAt != nil {
		t.Errorf("the saved card arrived read (%d); saving's read belongs to the Front Page", *later.Items[0].ReadAt)
	}

	// A second save is on the page the next time it is looked at, not at its next turn.
	second := front.Items[1]
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+second.ID+"/saved", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodGet, "/api/edition?page="+saved.Slug, nil), http.StatusOK, &later)
	if len(later.Items) != 2 {
		t.Fatalf("after a second save the saved page holds %d articles, want 2", len(later.Items))
	}

	// Each page reads for itself: read on the saved page, unread on the Front Page, and both hold.
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+second.ID+"/saved/read", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodDelete, "/api/edition/items/"+second.ID+"/read", nil), http.StatusNoContent, nil)
	readOn := func(path string) bool {
		t.Helper()
		var page editionBody
		h.expect(h.do(http.MethodGet, path, nil), http.StatusOK, &page)
		for _, item := range page.Items {
			if item.ID == second.ID {
				return item.ReadAt != nil
			}
		}
		t.Fatalf("%q is not on %s", second.Title, path)
		return false
	}
	if !readOn("/api/edition?page=" + saved.Slug) {
		t.Error("read on the saved page, and it does not say so there")
	}
	if readOn("/api/edition") {
		t.Error("reading on the saved page greyed it on the Front Page")
	}

	// Let go of, it stays on the page in front of you and says it is no longer kept. Unsaving
	// leaves the read mark alone: it was read where it was found, and still was.
	h.expect(h.do(http.MethodDelete, "/api/edition/items/"+kept.ID+"/saved", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodGet, "/api/edition?page="+saved.Slug, nil), http.StatusOK, &later)
	if len(later.Items) != 2 {
		t.Fatalf("unsaving took the page from 2 articles to %d; it should wait for the next one", len(later.Items))
	}
	for _, item := range later.Items {
		if item.ID == kept.ID && item.SavedAt != nil {
			t.Errorf("%q still says it is saved after unsaving", item.Title)
		}
	}
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &front)
	for _, item := range front.Items {
		if item.ID == kept.ID && item.ReadAt == nil {
			t.Error("unsaving took the read mark away")
		}
	}
}
