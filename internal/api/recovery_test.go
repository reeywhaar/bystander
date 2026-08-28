package api

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The link as a message writes it, so a test reads it the way its recipient would rather
// than out of a database that only holds its hash.
var recoveryURL = regexp.MustCompile(`https?://\S+/recover/(\S+)`)

func TestAnAdministratorIssuesARecoveryLink(t *testing.T) {
	h := newHarness(t)
	h.signIn("admin", "root")
	h.acceptAs("user", "alice")

	alice, err := h.store.PrincipalByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatalf("PrincipalByUsername(): %v", err)
	}

	var minted recoveryLinkBody
	h.expect(h.do(http.MethodPost, "/api/admin/users/"+alice.ID+"/recovery", map[string]string{}),
		http.StatusCreated, &minted)

	if minted.Username != "alice" {
		t.Errorf("username = %q, want alice", minted.Username)
	}
	token := strings.TrimPrefix(minted.URL, h.cfg.Link("/recover/"))
	if token == minted.URL || token == "" {
		t.Fatalf("url = %q, which is not a recovery link", minted.URL)
	}

	// Nothing has happened to the account yet: an administrator answering "I am locked out"
	// must not lock out somebody who turns out to have been fine.
	h.doAs(h.signInElsewhere("alice", harnessPassword), http.MethodGet, "/api/me", nil).Body.Close()

	var state recoveryBody
	h.expect(h.do(http.MethodGet, "/api/recoveries/"+token, nil), http.StatusOK, &state)
	if !state.Usable || state.Username != "alice" {
		t.Fatalf("link reads as %+v", state)
	}

	h.expect(h.do(http.MethodPost, "/api/recoveries/"+token+"/accept",
		map[string]string{"password": "battery-staple"}), http.StatusNoContent, nil)

	// The new password works and the old one does not, and nobody was signed in by the act
	// of setting it — the login form is the confirmation that the right person has it.
	h.signInElsewhere("alice", "battery-staple")
	res := h.do(http.MethodPost, "/api/login", map[string]string{
		"username": "alice", "password": harnessPassword})
	h.expect(res, http.StatusUnauthorized, nil)
}

func TestARecoveryLinkIsAnAdministratorsToIssue(t *testing.T) {
	h := newHarness(t)
	h.signIn("admin", "root")
	alice := h.acceptAs("user", "alice")

	root, err := h.store.PrincipalByUsername(t.Context(), "root")
	if err != nil {
		t.Fatalf("PrincipalByUsername(): %v", err)
	}
	h.expect(h.doAs(alice, http.MethodPost, "/api/admin/users/"+root.ID+"/recovery",
		map[string]string{}), http.StatusForbidden, nil)
}

// The whole of what keeps the forgotten-password form from being a way to ask this instance
// who has an account here.
func TestForgottenPasswordSaysTheSameThingWhateverHappens(t *testing.T) {
	h := newHarness(t)
	h.signIn("user", "alice")
	relay := h.relay()
	h.confirmRecovery(t, relay, "alice@example.com")
	before := relay.count()

	for _, address := range []string{
		"alice@example.com",  // an account that can be recovered
		"nobody@example.com", // no account at all
		"root@example.com",   // an account with no recovery address — root has none
	} {
		h.expect(h.do(http.MethodPost, "/api/recoveries",
			map[string]string{"email": address}), http.StatusNoContent, nil)
	}

	if got := relay.count() - before; got != 1 {
		t.Errorf("%d messages went out, want 1 — only the address on file has anywhere to send to", got)
	}
	sent := relay.lastTo(t, "alice@example.com")
	// The mail names the account, which is the only correction available to somebody who
	// typed the wrong address: a link arriving for a name you do not recognise is its own
	// answer, and the form itself can say nothing.
	if !strings.Contains(sent.Body, "alice") {
		t.Errorf("the message does not name the account:\n%s", sent.Body)
	}
}

func TestAMailedRecoveryLinkWorks(t *testing.T) {
	h := newHarness(t)
	h.signIn("user", "alice")
	relay := h.relay()
	h.confirmRecovery(t, relay, "alice@example.com")

	h.expect(h.do(http.MethodPost, "/api/recoveries",
		map[string]string{"email": "alice@example.com"}), http.StatusNoContent, nil)

	link := recoveryURL.FindStringSubmatch(relay.lastTo(t, "alice@example.com").Body)
	if link == nil {
		t.Fatalf("no recovery link in the message:\n%s", relay.lastTo(t, "alice@example.com").Body)
	}

	h.expect(h.do(http.MethodPost, "/api/recoveries/"+link[1]+"/accept",
		map[string]string{"password": "battery-staple"}), http.StatusNoContent, nil)
	h.signInElsewhere("alice", "battery-staple")

	// And the session that asked for it is gone with the rest: the reason to be here at all
	// is usually that somebody else has one.
	h.expect(h.do(http.MethodGet, "/api/me", nil), http.StatusUnauthorized, nil)
}

// The login form asks this before it offers anything, because a form that takes an address
// and says "check your inbox" on an instance that cannot send is lying.
func TestThePublicInstanceSaysWhetherRecoveryIsOnOffer(t *testing.T) {
	h := newHarness(t)

	var body publicInstanceBody
	h.expect(h.do(http.MethodGet, "/api/instance", nil), http.StatusOK, &body)
	if body.Recovery {
		t.Error("recovery is offered with no relay configured")
	}

	h.relay()
	h.expect(h.do(http.MethodGet, "/api/instance", nil), http.StatusOK, &body)
	if !body.Recovery {
		t.Error("recovery is not offered with a relay configured")
	}
}

func TestAnUnknownRecoveryLinkIsRefused(t *testing.T) {
	h := newHarness(t)

	h.expect(h.do(http.MethodGet, "/api/recoveries/not-a-token", nil), http.StatusNotFound, nil)
	h.expect(h.do(http.MethodPost, "/api/recoveries/not-a-token/accept",
		map[string]string{"password": "battery-staple"}), http.StatusNotFound, nil)
}
