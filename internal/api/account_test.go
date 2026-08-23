package api

import (
	"net/http"
	"testing"

	"bystander/internal/store"
)

func TestAccountSaysWhetherMailCouldReachIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.Username != "alice" || body.RecoveryEmail != "" {
		t.Fatalf("account = %+v", body)
	}
	// No relay yet, so an address stored here could not be sent to — and the page has to
	// be able to say so rather than implying recovery it cannot perform.
	if body.MailConfigured {
		t.Error("claims mail works with no relay configured")
	}

	h.expect(h.do(http.MethodPut, "/api/admin/smtp", map[string]any{
		"host": "smtp.example.com", "port": 587, "tls": "starttls",
		"username": "operator", "password": "hunter2", "from_address": "paper@example.com",
	}), http.StatusOK, nil)

	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if !body.MailConfigured {
		t.Error("a configured relay went unnoticed")
	}
}

func TestRecoveryEmailRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var body accountBody
	// A display name is accepted and only the address is kept: what goes in a To header
	// is the address, and storing the rest would store something that is never used.
	h.expect(h.do(http.MethodPatch, "/api/account",
		map[string]any{"recovery_email": "Alice <alice@example.com>"}), http.StatusOK, &body)
	if body.RecoveryEmail != "alice@example.com" {
		t.Fatalf("recovery_email = %q", body.RecoveryEmail)
	}

	// Empty clears it. "No address" and "the empty address" are the same thing.
	h.expect(h.do(http.MethodPatch, "/api/account",
		map[string]any{"recovery_email": ""}), http.StatusOK, &body)
	if body.RecoveryEmail != "" {
		t.Errorf("recovery_email = %q, want it cleared", body.RecoveryEmail)
	}

	h.expect(h.do(http.MethodPatch, "/api/account",
		map[string]any{"recovery_email": "not an address"}), http.StatusBadRequest, nil)
	h.expect(h.do(http.MethodPatch, "/api/account", map[string]any{}), http.StatusBadRequest, nil)
}

func TestChangingAPasswordNeedsTheCurrentOne(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	// A session cookie is enough to read somebody's feeds. It must not also be enough to
	// take the account — knowing the current password is the one thing an attacker
	// holding a borrowed cookie does not.
	h.expect(h.do(http.MethodPost, "/api/account/password", map[string]any{
		"current_password": "not-it", "new_password": "a-brand-new-one",
	}), http.StatusBadRequest, nil)

	h.expect(h.do(http.MethodPost, "/api/account/password", map[string]any{
		"current_password": "", "new_password": "a-brand-new-one",
	}), http.StatusBadRequest, nil)

	// And the old one still works, because nothing changed.
	h.expect(h.do(http.MethodPost, "/api/login", map[string]any{
		"username": "alice", "password": harnessPassword,
	}), http.StatusNoContent, nil)
}

func TestChangingAPasswordKeepsThisSessionAndEndsTheOthers(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	// A second sign-in, as if from a phone. Its cookie is captured before this session
	// takes the jar back over.
	elsewhere := h.signInElsewhere("alice", harnessPassword)

	h.expect(h.do(http.MethodPost, "/api/account/password", map[string]any{
		"current_password": harnessPassword, "new_password": "a-brand-new-one",
	}), http.StatusNoContent, nil)

	// Still signed in here: being asked to sign in again immediately after proving who
	// you are is a strange way to confirm it worked.
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)

	// The other one is gone, which is what "signs out my other devices" means.
	res := h.doAs(elsewhere, http.MethodGet, "/api/account", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the other session survived: %d", res.StatusCode)
	}

	// And the new password is the one that works now.
	h.expect(h.do(http.MethodPost, "/api/login", map[string]any{
		"username": "alice", "password": harnessPassword,
	}), http.StatusUnauthorized, nil)
	h.expect(h.do(http.MethodPost, "/api/login", map[string]any{
		"username": "alice", "password": "a-brand-new-one",
	}), http.StatusNoContent, nil)
}

func TestAccountIsYourOwnOnly(t *testing.T) {
	h := newHarness(t)

	// Nothing here is reachable without a session: it is somebody's own account.
	for _, call := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/account"},
		{http.MethodPatch, "/api/account"},
		{http.MethodPost, "/api/account/password"},
	} {
		res := h.do(call.method, call.path, map[string]any{})
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", call.method, call.path, res.StatusCode)
		}
		res.Body.Close()
	}
}
