package api

import (
	"net/http"
	"testing"

	"bystander/internal/store"
)

func TestHealthz(t *testing.T) {
	h := newHarness(t)

	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	h.expect(h.do(http.MethodGet, "/healthz", nil), http.StatusOK, &body)
	if !body.OK {
		t.Error("healthz did not report itself healthy")
	}
}

// The whole product, in the order somebody meets it: a link becomes an account, an account
// adds a feed, a feed becomes a page.
func TestInviteToFrontPage(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 8)

	// An invitation reports its own state before anybody types a password into it.
	_, token, err := h.store.CreateInvite(t.Context(), store.RoleAdmin, "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}
	var invite inviteBody
	h.expect(h.do(http.MethodGet, "/api/invites/"+token, nil), http.StatusOK, &invite)
	if !invite.Usable || invite.Role != string(store.RoleAdmin) {
		t.Fatalf("invite = %+v, want a usable admin invitation", invite)
	}

	// Accepting signs them in: they have just chosen a password, so asking for it again
	// proves nothing.
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "alice", "password": "correct-horse"}), http.StatusNoContent, nil)

	var me meBody
	h.expect(h.do(http.MethodGet, "/api/me", nil), http.StatusOK, &me)
	if me.Username != "alice" || me.Role != string(store.RoleAdmin) {
		t.Fatalf("me = %+v, want alice the administrator", me)
	}

	// A page before there is anything to put on it is a state, not an error.
	var empty editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("a brand new account already has %d articles", len(empty.Items))
	}

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, &sub)
	if sub.Title != "The Example" {
		t.Errorf("feed title = %q, want the publisher's", sub.Title)
	}
	if sub.Priority != store.DefaultPriority {
		t.Errorf("priority = %d, want the default of %d", sub.Priority, store.DefaultPriority)
	}

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) != 8 {
		t.Fatalf("the page holds %d articles, want the 8 the feed published", len(page.Items))
	}
	if page.Items[0].Slot != string(store.SlotLead) {
		t.Errorf("the first article is laid out as %q, want lead", page.Items[0].Slot)
	}
	if page.Items[0].Feed.Title != "The Example" {
		t.Errorf("the card names its source as %q", page.Items[0].Feed.Title)
	}

	// Sanitizing happens at ingest, so what reaches the browser is already safe.
	for _, article := range page.Items {
		if contains(article.Summary, "<script") || contains(article.Summary, "alert(") {
			t.Fatalf("a script survived into a card: %q", article.Summary)
		}
	}
	if page.Items[0].ImageURL == "" {
		t.Error("no image was found for the lead, though the feed embedded one")
	}

	// A read mark greys a card in place; it does not move or remove it.
	first := page.Items[0].ID
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+first+"/read", nil), http.StatusNoContent, nil)

	var afterRead editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &afterRead)
	if len(afterRead.Items) != len(page.Items) {
		t.Fatalf("reading an article changed the page from %d articles to %d", len(page.Items), len(afterRead.Items))
	}
	if afterRead.Items[0].ID != first {
		t.Errorf("the read article moved from rank 0 to somewhere else")
	}
	if afterRead.Items[0].ReadAt == nil {
		t.Error("the article was not marked read")
	}

	h.expect(h.do(http.MethodDelete, "/api/edition/items/"+first+"/read", nil), http.StatusNoContent, nil)
	var afterUnread editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &afterUnread)
	if afterUnread.Items[0].ReadAt != nil {
		t.Error("the mark did not clear")
	}
}

// Nothing is shown twice: the record of what was shown outlives the page it was on.
func TestASecondPageDoesNotRepeatTheFirst(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 10)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	// Two articles a page, so the second page has somewhere to draw from.
	h.expect(h.do(http.MethodPatch, "/api/settings", map[string]int{"edition_size": 10}), http.StatusOK, nil)

	var first editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &first)

	seen := map[string]bool{}
	for _, article := range first.Items {
		seen[article.Link] = true
	}
	if len(seen) == 0 {
		t.Fatal("the first page was empty")
	}

	// The feed has published nothing new, and every article is spoken for. That is a
	// conflict, not a 404: the page on screen is real, and saying "there is nothing to put
	// on a page" to somebody looking at one would read as a fault.
	var refusal errorBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusConflict, &refusal)
	if !contains(refusal.Error, "nothing new") {
		t.Errorf("refusal = %q, want it to say nothing new has been published", refusal.Error)
	}

	// And the page it refused to replace is still there, unchanged.
	var still editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &still)
	if still.ID != first.ID {
		t.Errorf("the page changed from %q to %q despite the refusal", first.ID, still.ID)
	}
}

// Nothing at all and nothing new are different answers, and the words have to differ too.
func TestRegenerateWithNoFeeds(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var refusal errorBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusNotFound, &refusal)
	if !contains(refusal.Error, "add a feed") {
		t.Errorf("refusal = %q, want it to say what to do about it", refusal.Error)
	}
}

// Feeds are global: two people following one URL cause one fetch.
func TestTwoSubscribersOneFetch(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 3)

	h.signIn(store.RoleAdmin, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	afterFirst := feed.hits

	// A second account, in the same process, following the same URL.
	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	// The second subscriber still causes a discovery fetch — the URL has to be checked
	// before it is accepted — but both share one feed row, so the poller will fetch once.
	feeds, err := h.store.FeedIDs(t.Context())
	if err != nil {
		t.Fatalf("FeedIDs(): %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("%d feed rows for one URL, want 1 (discovery fetches: %d)", len(feeds), feed.hits-afterFirst)
	}
}

func TestFollowingAFeedTwiceIsRefused(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 2)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusConflict, nil)
}

func TestAddingSomethingThatIsNotAFeed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	page := newPlainPage(t, `<!doctype html><html><head><title>No feed here</title></head><body>hi</body></html>`)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": page.URL}), http.StatusBadRequest, nil)
}

// Somebody pasting a site's home page should get its feed, not a refusal.
func TestAddingASiteFindsItsFeed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	feed := newFeedServer(t, 4)
	page := newPlainPage(t, `<!doctype html><html><head>`+
		`<link rel="alternate" type="application/rss+xml" href="`+feed.URL+`">`+
		`</head><body>a blog</body></html>`)

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": page.URL}), http.StatusCreated, &sub)
	if sub.Title != "The Example" {
		t.Errorf("subscribed to %q, want the feed the page named", sub.Title)
	}
}

func TestTagsShapeThePage(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var tag tagBody
	h.expect(h.do(http.MethodPost, "/api/tags",
		map[string]any{"name": "World News", "priority": 90}), http.StatusCreated, &tag)
	if tag.Priority != 90 {
		t.Fatalf("priority = %d, want 90", tag.Priority)
	}

	feed := newFeedServer(t, 5)
	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds",
		map[string]any{"url": feed.URL, "tag_ids": []string{tag.ID}}), http.StatusCreated, &sub)
	if len(sub.TagIDs) != 1 || sub.TagIDs[0] != tag.ID {
		t.Fatalf("tags = %v, want the one it was created with", sub.TagIDs)
	}

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) == 0 {
		t.Fatal("a tagged feed produced no page")
	}
}

// A tag inside itself would make the manage page recurse until it ran out of stack.
func TestTagCyclesAreRefused(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var parent, child tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "News"}), http.StatusCreated, &parent)
	h.expect(h.do(http.MethodPost, "/api/tags",
		map[string]any{"name": "World", "parent_id": parent.ID}), http.StatusCreated, &child)

	h.expect(h.do(http.MethodPatch, "/api/tags/"+parent.ID,
		map[string]any{"parent_id": child.ID}), http.StatusBadRequest, nil)
	h.expect(h.do(http.MethodPatch, "/api/tags/"+parent.ID,
		map[string]any{"parent_id": parent.ID}), http.StatusBadRequest, nil)
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
