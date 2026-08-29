package api

import (
	"net/http"
	"testing"
	"time"

	"bystander/internal/store"
)

// stranger is a client with no cookies at all — not signed out, never signed in.
//
// The distinction matters here: a published page is served to somebody who has never had an
// account, and a test that merely dropped its own session would still be exercising a browser
// that had once been given one.
func (h *harness) stranger() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// allowPublishing turns the instance's switches on, which start off.
func (h *harness) allowPublishing(t *testing.T, indexing bool) {
	t.Helper()
	if err := h.store.SetInstance(t.Context(), store.InstanceSettings{
		PublicPages: true, PublicIndexing: indexing,
	}); err != nil {
		t.Fatalf("SetInstance(): %v", err)
	}
}

// A published page is served to anybody, and to nobody before it is published.
func TestAPageIsPublishedAndTakenDown(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 8)

	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)

	// Nothing is there until somebody publishes something.
	h.expect(h.doAs(h.stranger(), http.MethodGet, "/api/public/misha/front", nil), http.StatusNotFound, nil)

	var page pageBody
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, &page)
	if !page.Published || page.PublishSlug != "front" {
		t.Fatalf("page = %+v, want it published at front", page)
	}

	// And then it is, to somebody with no session at all.
	nobody := h.stranger()
	var public publicPageBody
	h.expect(h.doAs(nobody, http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, &public)
	if len(public.Items) == 0 {
		t.Fatal("the published page is empty")
	}
	if public.Name == "" {
		t.Error("the published page has no name")
	}
	// The owner's reading is not published with it. Whether they have read something is a
	// fact about them, and publishing a page is not an offer to publish that too.
	for _, item := range public.Items {
		if item.ReadAt != nil {
			t.Errorf("%q carries the owner's read mark", item.Title)
			break
		}
	}

	// Taken down, and gone again.
	h.expect(h.do(http.MethodDelete, "/api/pages/"+h.mainPage()+"/publish", nil), http.StatusOK, &page)
	if page.Published {
		t.Error("the page is still published")
	}
	// The address is kept, so publishing it again offers the one the links point at.
	if page.PublishSlug != "front" {
		t.Errorf("publish slug = %q, want it remembered", page.PublishSlug)
	}
	h.expect(h.doAs(nobody, http.MethodGet, "/api/public/misha/front", nil), http.StatusNotFound, nil)
}

// The instance decides whether it serves anything to strangers, and the answer is not a
// default for pages to inherit: turning it off takes what is already up down.
func TestTheInstanceCanRefusePublishingAltogether(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)

	// Off until an administrator says otherwise, which is what a fresh instance is.
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusForbidden, nil)

	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, nil)
	nobody := h.stranger()
	h.expect(h.doAs(nobody, http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, nil)

	// Turned off again, everything already up goes down with it.
	if err := h.store.SetInstance(t.Context(), store.InstanceSettings{}); err != nil {
		t.Fatal(err)
	}
	h.expect(h.doAs(nobody, http.MethodGet, "/api/public/misha/front", nil), http.StatusNotFound, nil)
}

// A page cannot be published by somebody with nowhere to publish it to, and the refusal says
// which of the two problems it is — one the person can fix, one they cannot.
func TestPublishingNeedsAPublicNameFirst(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)

	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusConflict, nil)
}

// Indexing needs both answers, and the instance's is the ceiling.
func TestAPageIsOnlyIndexableIfTheInstanceAllowsIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)

	// Asked for, and refused by the instance rather than by an error: the interface does not
	// offer the control here, and a request that arrives anyway is not honoured.
	var page pageBody
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front", "indexable": true}), http.StatusOK, &page)
	if page.Indexable {
		t.Error("a page is indexable on an instance that does not allow indexing")
	}

	// Allowed, and asked for, and so it is.
	h.allowPublishing(t, true)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front", "indexable": true}), http.StatusOK, &page)
	if !page.Indexable {
		t.Error("a page is not indexable although both said yes")
	}
}

// Giving up the public name takes the pages down with it: the name is what the addresses are
// built from, and keeping them up would leave them answering at an address nothing produces.
func TestGivingUpAPublicNameTakesThePagesDown(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, nil)

	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": ""}), http.StatusOK, nil)

	var page pageBody
	h.expect(h.do(http.MethodGet, "/api/pages/"+h.mainPage(), nil), http.StatusOK, &page)
	if page.Published {
		t.Error("a page is still published after its owner gave up their name")
	}
}

// Publishing is the owner's to do, and the instance's settings are the administrator's.
func TestPublishingBelongsToTheOwner(t *testing.T) {
	h := newHarness(t)

	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)
	alicePage := h.mainPage()

	h.signIn(store.RoleUser, "bob")
	// Somebody else's page is not found rather than forbidden: whether it exists is not
	// Bob's business.
	h.expect(h.do(http.MethodPut, "/api/pages/"+alicePage+"/publish",
		map[string]any{"slug": "mine"}), http.StatusNotFound, nil)
	h.expect(h.do(http.MethodGet, "/api/admin/instance", nil), http.StatusForbidden, nil)
	h.expect(h.do(http.MethodPut, "/api/admin/instance",
		map[string]any{"public_pages": true}), http.StatusForbidden, nil)
}

// A signed-in visitor reads somebody else's page, and their own marks are their own.
//
// Two things have to be true at once and they pull in opposite directions. The owner's reading
// is never shown — whether they have read something is a fact about them, and publishing a page
// is not an offer to publish that too. And a visitor's own reading *is* shown, because a read
// mark is a fact about a person and an article and does not care whose page it was seen on.
func TestAVisitorSeesTheirOwnReadingAndNotTheOwners(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 6)

	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	var alicePage editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &alicePage)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, nil)

	// Alice reads one of her own.
	hers := alicePage.Items[0].ID
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+hers+"/read", nil), http.StatusNoContent, nil)

	// Bob arrives. He sees the page, and none of Alice's reading.
	bob := h.signInAsSomebodyElse("bob")
	var seen publicPageBody
	h.expect(h.doAs(bob, http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, &seen)
	if !seen.SignedIn {
		t.Error("a signed-in visitor is reported as a stranger")
	}
	for _, item := range seen.Items {
		if item.ReadAt != nil {
			t.Fatalf("%q carries somebody else's read mark", item.Title)
		}
	}

	// He marks one read, through the same endpoint he would use on his own page — a read
	// mark is a fact about a person and an article, and whose page it was seen on does not
	// come into it.
	his := seen.Items[1].ID
	h.expect(h.doAs(bob, http.MethodPut,
		"/api/edition/items/"+his+"/read", nil), http.StatusNoContent, nil)

	h.expect(h.doAs(bob, http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, &seen)
	marks := map[string]bool{}
	for _, item := range seen.Items {
		marks[item.ID] = item.ReadAt != nil
	}
	if !marks[his] {
		t.Error("the visitor's own mark is not shown back to them")
	}
	if marks[hers] {
		t.Error("the owner's mark leaked to a visitor")
	}

	// And Alice's own page is untouched by any of it: hers still read, Bob's still not.
	var afterwards editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &afterwards)
	for _, item := range afterwards.Items {
		if item.ID == hers && item.ReadAt == nil {
			t.Error("the owner's own read mark was cleared by a visitor")
		}
		if item.ID == his && item.ReadAt != nil {
			t.Error("a visitor's read mark appeared on the owner's page")
		}
	}
}

// The reader can act on the feed behind a card — show more of it, less of it, be done with it
// — and the stub on the article is what carries the subscription that makes it possible.
//
// A published page builds those stubs from the *owner's* subscriptions, because their own
// names for their feeds are what appear on their page. So this asserts the half that must not
// come with it: an id belonging to somebody else's account, handed to whoever opens a link.
func TestAPublishedPageCarriesNoSubscriptionOfTheOwners(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)

	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, nil)

	// Her own page carries it, which is what the reader acts through.
	var hers editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &hers)
	if len(hers.Items) == 0 {
		t.Fatal("no articles on the owner's page")
	}
	if hers.Items[0].Feed.SubscriptionID == "" {
		t.Error("the owner's own page carries no subscription to act on")
	}

	var seen publicPageBody
	h.expect(h.do(http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, &seen)
	for _, item := range seen.Items {
		if item.Feed.SubscriptionID != "" {
			t.Fatalf("%q hands out the owner's subscription %q", item.Title, item.Feed.SubscriptionID)
		}
	}
}

// A stranger cannot mark anything: reading is a fact about a person, and a stranger is nobody.
func TestOnlySomebodyWithAnAccountMarksAnythingRead(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)

	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, nil)

	nobody := h.stranger()
	h.expect(h.doAs(nobody, http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, nil)
	h.expect(h.doAs(nobody, http.MethodPut,
		"/api/edition/items/"+page.Items[0].ID+"/read", nil), http.StatusUnauthorized, nil)
}

// A published page is the page, down to how each card looks.
//
// Every card's appearance — its voice, its width, whether it is boxed — is drawn by the client
// from the edition's id and the article's, so the id is not incidental metadata here: it is
// what makes a stranger's copy of the page the same object as the owner's. Publishing a page
// that carries the same articles under different faces produces a second page that happens to
// have the same contents, which is not what publishing means.
//
// This was wrong once. The public body left the id out and the client seeded itself on the
// composition time instead, which is stable — the same visitor saw the same thing twice — and
// completely different from what the owner saw. Stability is not the property that was needed.
func TestAPublishedPageIsDrawnFromTheSameEdition(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 8)

	h.signIn(store.RoleUser, "alice")
	h.allowPublishing(t, false)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	var owner editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &owner)
	h.expect(h.do(http.MethodPut, "/api/account/public-name", map[string]string{"name": "misha"}), http.StatusOK, nil)
	h.expect(h.do(http.MethodPut, "/api/pages/"+h.mainPage()+"/publish",
		map[string]any{"slug": "front"}), http.StatusOK, nil)

	var public publicPageBody
	h.expect(h.doAs(h.stranger(), http.MethodGet, "/api/public/misha/front", nil), http.StatusOK, &public)

	if public.ID == "" {
		t.Fatal("the published page carries no edition id, so a visitor cannot draw the page the owner sees")
	}
	if public.ID != owner.ID {
		t.Errorf("published id = %q, owner's = %q", public.ID, owner.ID)
	}

	// The other half of the same seed. Both ids go into the draw, so the articles have to
	// arrive in the same order under the same ids for the pairs to match up.
	if len(public.Items) != len(owner.Items) {
		t.Fatalf("published %d items, the owner sees %d", len(public.Items), len(owner.Items))
	}
	for i, item := range public.Items {
		if item.ID != owner.Items[i].ID {
			t.Errorf("item %d: published %q, owner's %q", i, item.ID, owner.Items[i].ID)
		}
	}
}
