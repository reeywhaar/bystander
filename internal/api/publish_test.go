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
