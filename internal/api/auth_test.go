package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"bystander/internal/session"
	"bystander/internal/store"
)

func TestSignedOutIsRefused(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/api/me", "/api/edition", "/api/feeds", "/api/tags", "/api/pages"} {
		res := h.do(http.MethodGet, path, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s while signed out answered %d, want 401", path, res.StatusCode)
		}
		// A 401 and a JSON body, never a redirect: a 302 to an HTML page is the least
		// useful thing a fetch can receive.
		if location := res.Header.Get("Location"); location != "" {
			t.Errorf("GET %s redirected to %q instead of refusing", path, location)
		}
	}
}

func TestLoginAndLogout(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)

	res := h.do(http.MethodGet, "/api/me", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("still signed in after logging out: %d", res.StatusCode)
	}

	h.expect(h.do(http.MethodPost, "/api/login",
		map[string]string{"username": "alice", "password": "correct-horse"}), http.StatusNoContent, nil)

	var me meBody
	h.expect(h.do(http.MethodGet, "/api/me", nil), http.StatusOK, &me)
	if me.Username != "alice" {
		t.Errorf("signed in as %q", me.Username)
	}
}

func TestTheSessionCookieIsProtected(t *testing.T) {
	h := newHarness(t)

	_, token, err := h.store.CreateInvite(t.Context(), store.RoleUser, "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}
	res := h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "alice", "password": "correct-horse"})
	defer res.Body.Close()

	var found *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == session.CookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("no session cookie was set")
	}
	if !found.HttpOnly {
		t.Error("the session cookie is readable by script")
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", found.SameSite)
	}
	// The harness runs on http, so Secure is correctly absent. What must never happen is
	// the cookie's value being something already in the database.
	if _, err := h.store.SessionByToken(t.Context(), found.Value); err != nil {
		t.Fatalf("the cookie does not resolve to a session: %v", err)
	}
}

// A wrong name and a wrong password must be indistinguishable, or the login form becomes a
// list of who has an account here.
func TestLoginRefusalsAreIdentical(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)

	var wrongPassword, unknownName errorBody
	h.expect(h.do(http.MethodPost, "/api/login",
		map[string]string{"username": "alice", "password": "wrong"}), http.StatusUnauthorized, &wrongPassword)
	h.expect(h.do(http.MethodPost, "/api/login",
		map[string]string{"username": "nobody", "password": "wrong"}), http.StatusUnauthorized, &unknownName)

	if wrongPassword.Error != unknownName.Error {
		t.Errorf("a wrong password says %q but an unknown name says %q", wrongPassword.Error, unknownName.Error)
	}
}

func TestAdminRoutesRefuseOrdinaryAccounts(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	for _, path := range []string{"/api/admin/users", "/api/admin/invites"} {
		res := h.do(http.MethodGet, path, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s as an ordinary account answered %d, want 403", path, res.StatusCode)
		}
	}
}

func TestAdminMintsAndWithdrawsInvitations(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	var minted adminInviteBody
	h.expect(h.do(http.MethodPost, "/api/admin/invites",
		map[string]string{"role": "user"}), http.StatusCreated, &minted)

	// The link is returned exactly once, here, and it is built from the public URL rather
	// than from anything a client sent.
	if minted.URL == nil || !strings.HasPrefix(*minted.URL, h.cfg.PublicURL.String()+"/invite/") {
		t.Fatalf("invitation URL = %v, want one under the public URL", minted.URL)
	}

	var listed []adminInviteBody
	h.expect(h.do(http.MethodGet, "/api/admin/invites", nil), http.StatusOK, &listed)
	for _, inv := range listed {
		if inv.URL != nil {
			t.Error("a listing handed the token back out; it is meant to be unrecoverable")
		}
	}

	h.expect(h.do(http.MethodDelete, "/api/admin/invites/"+minted.ID, nil), http.StatusNoContent, nil)
}

// There is no recovery path that does not involve a shell on the host.
func TestTheLastAdminCannotBeRemoved(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	var users []userBody
	h.expect(h.do(http.MethodGet, "/api/admin/users", nil), http.StatusOK, &users)
	if len(users) != 1 {
		t.Fatalf("%d accounts, want 1", len(users))
	}

	// Their own account, which is refused for that reason before the last-admin rule is
	// even reached.
	h.expect(h.do(http.MethodDelete, "/api/admin/users/"+users[0].ID, nil), http.StatusConflict, nil)
	h.expect(h.do(http.MethodPatch, "/api/admin/users/"+users[0].ID,
		map[string]bool{"disabled": true}), http.StatusConflict, nil)
}

func TestDisablingAnAccountEndsItsSession(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	bob, err := h.store.CreatePrincipal(t.Context(), "bob", "correct-horse", store.RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if err := h.store.CreateSession(t.Context(), "bobs-token", bob.ID, h.store.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	h.expect(h.do(http.MethodPatch, "/api/admin/users/"+bob.ID,
		map[string]bool{"disabled": true}), http.StatusNoContent, nil)

	if _, err := h.store.SessionByToken(t.Context(), "bobs-token"); err == nil {
		t.Fatal("a disabled account kept its session")
	}
}

// One person must not be able to touch another's page, tags or feeds — the ids are opaque,
// but "unguessable" is not an authorisation model.
func TestOnePersonCannotReachAnother(t *testing.T) {
	h := newHarness(t)

	h.signIn(store.RoleUser, "alice")
	var aliceTag tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Art"}), http.StatusCreated, &aliceTag)

	feed := newFeedServer(t, 3)
	var aliceFeed subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, &aliceFeed)

	var alicePage editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &alicePage)

	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")

	h.expect(h.do(http.MethodGet, "/api/feeds/"+aliceFeed.ID, nil), http.StatusNotFound, nil)
	h.expect(h.do(http.MethodDelete, "/api/feeds/"+aliceFeed.ID, nil), http.StatusNotFound, nil)
	h.expect(h.do(http.MethodPatch, "/api/tags/"+aliceTag.ID,
		map[string]any{"name": "Stolen"}), http.StatusNotFound, nil)
	h.expect(h.do(http.MethodDelete, "/api/tags/"+aliceTag.ID, nil), http.StatusNotFound, nil)
	// Marking an article read is the exception, and it is not a hole.
	//
	// It used to be refused, back when a read mark was a column on the edition and writing
	// one meant writing to a particular page. It is now a fact about a person and an
	// article, stored once against whoever did the reading — so Bob marking Alice's article
	// records that *Bob* read it and touches nothing of hers. Which is also what lets one
	// endpoint serve a page somebody else published.
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+alicePage.Items[0].ID+"/read", nil),
		http.StatusNoContent, nil)

	// The part that matters: Alice's own page is exactly as she left it.
	alice := h.signInElsewhere("alice", harnessPassword)
	var hers editionBody
	h.expect(h.doAs(alice, http.MethodGet, "/api/edition", nil), http.StatusOK, &hers)
	for _, item := range hers.Items {
		if item.ReadAt != nil {
			t.Errorf("%q was marked read on Alice's page by somebody else", item.Title)
		}
	}
}
