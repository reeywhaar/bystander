package api

import (
	"errors"
	"net/http"
	"strings"
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

func TestRecoveryNeedsARelayBeforeItNeedsAnything(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	// Refused before anything is written, so nobody ends up holding a code for a flow that
	// could never have finished.
	h.expect(h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "alice@example.com"}), http.StatusConflict, nil)

	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryPending != "" {
		t.Errorf("a refused start left %q waiting", body.RecoveryPending)
	}
}

func TestARecoveryCodeThatCouldNotBeSentLeavesNothingWaiting(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	relay := h.relay()
	relay.refuse = errors.New("the relay rejected the credentials: 535")

	res := h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "alice@example.com"})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.StatusCode)
	}

	// Nothing was sent, so nothing is waiting. Left behind, the row would have the account
	// page say it is waiting on a code that never left — and the way out of that state is
	// the button somebody just watched fail.
	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryPending != "" {
		t.Errorf("recovery_pending = %q after a send that failed", body.RecoveryPending)
	}
}

func TestAFailedChangeLeavesTheAddressThatAlreadyWorked(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	relay := h.relay()

	h.expect(h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "alice@example.com"}), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
		map[string]any{"code": relay.codeSentTo(t, "alice@example.com")}), http.StatusOK, nil)

	// Now a change that cannot be delivered. Undoing the attempt must not take the address
	// that already works with it — losing a working recovery address by trying to improve
	// on it is the worst outcome this whole flow exists to avoid.
	relay.refuse = errors.New("the relay rejected the message: 550")
	res := h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "elsewhere@example.com"})
	res.Body.Close()

	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryEmail != "alice@example.com" {
		t.Errorf("recovery_email = %q, want the one that already worked", body.RecoveryEmail)
	}
	if body.RecoveryPending != "" {
		t.Errorf("recovery_pending = %q after a send that failed", body.RecoveryPending)
	}
}

func TestRecoveryAddressIsNotOnRecordUntilItIsProved(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	relay := h.relay()

	h.expect(h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "Alice <alice@example.com>"}), http.StatusNoContent, nil)

	// Waiting on it, and only waiting. An address nobody has proved they can read is not
	// something this account can be recovered through.
	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryEmail != "" {
		t.Errorf("an unproved address went straight onto the record: %q", body.RecoveryEmail)
	}
	// A display name is accepted and only the address is kept: what goes in a To header is
	// the address, and keeping the rest would keep something that is never used.
	if body.RecoveryPending != "alice@example.com" {
		t.Fatalf("recovery_pending = %q", body.RecoveryPending)
	}

	code := relay.codeSentTo(t, "alice@example.com")

	// Wrong first, because that is the case that must not put anything on the record.
	h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
		map[string]any{"code": "AAAAAAAA"}), http.StatusBadRequest, nil)
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryEmail != "" {
		t.Fatalf("a wrong code confirmed the address anyway")
	}

	// Lower case is the same code: it is typed by hand off another screen.
	h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
		map[string]any{"code": strings.ToLower(code)}), http.StatusOK, &body)
	if body.RecoveryEmail != "alice@example.com" {
		t.Fatalf("recovery_email = %q", body.RecoveryEmail)
	}
	if body.RecoveryPending != "" {
		t.Errorf("recovery_pending = %q, want it cleared", body.RecoveryPending)
	}

	// And forgetting it takes both.
	h.expect(h.do(http.MethodDelete, "/api/account/recovery", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryEmail != "" || body.RecoveryPending != "" {
		t.Errorf("something survived being forgotten: %+v", body)
	}
}

func TestRecoveryCodeCanBeWorkedThroughOnlyFiveTimes(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	relay := h.relay()

	h.expect(h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "alice@example.com"}), http.StatusNoContent, nil)
	code := relay.codeSentTo(t, "alice@example.com")

	// Five wrong guesses is what makes eight characters enough.
	for range store.MaxRecoveryAttempts {
		h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
			map[string]any{"code": "AAAAAAAA"}), http.StatusBadRequest, nil)
	}

	// The attempt is thrown away rather than locked, so even the right code is now no use
	// — and starting again is one request.
	h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
		map[string]any{"code": code}), http.StatusBadRequest, nil)

	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryEmail != "" {
		t.Error("an exhausted attempt still confirmed")
	}
}

func TestProvingAnAddressTakesItFromWhoeverHadIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	relay := h.relay()

	h.expect(h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "shared@example.com"}), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
		map[string]any{"code": relay.codeSentTo(t, "shared@example.com")}), http.StatusOK, nil)

	// A second account, signed in through its own jar.
	_, token, err := h.store.CreateInvite(t.Context(), store.RoleUser, "")
	if err != nil {
		t.Fatal(err)
	}
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "bob", "password": harnessPassword}), http.StatusNoContent, nil)

	// Bob proves the same address — differently cased, because nobody thinks of those as
	// two addresses.
	h.expect(h.do(http.MethodPost, "/api/account/recovery",
		map[string]any{"email": "Shared@Example.com"}), http.StatusNoContent, nil)
	var bob accountBody
	h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
		map[string]any{"code": relay.codeSentTo(t, "Shared@Example.com")}), http.StatusOK, &bob)
	if bob.RecoveryEmail != "Shared@Example.com" {
		t.Fatalf("bob's recovery_email = %q", bob.RecoveryEmail)
	}

	// And Alice no longer has it. Whoever can read the inbox today is who recovery through
	// it would actually reach, and two accounts sharing one is two accounts a single inbox
	// can take.
	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodPost, "/api/login",
		map[string]any{"username": "alice", "password": harnessPassword}), http.StatusNoContent, nil)

	var alice accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &alice)
	if alice.RecoveryEmail != "" {
		t.Errorf("alice kept an address bob proved: %q", alice.RecoveryEmail)
	}
}

func TestReProvingYourOwnAddressIsNotATakeover(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	relay := h.relay()

	for range 2 {
		h.expect(h.do(http.MethodPost, "/api/account/recovery",
			map[string]any{"email": "alice@example.com"}), http.StatusNoContent, nil)
		h.expect(h.do(http.MethodPost, "/api/account/recovery/confirm",
			map[string]any{"code": relay.codeSentTo(t, "alice@example.com")}), http.StatusOK, nil)
	}

	var body accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &body)
	if body.RecoveryEmail != "alice@example.com" {
		t.Errorf("recovery_email = %q", body.RecoveryEmail)
	}
}

func TestRecoveryRefusesSomethingThatIsNotAnAddress(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	h.relay()

	for _, bad := range []string{"", "not an address", "alice@localhost", "alice@.com"} {
		res := h.do(http.MethodPost, "/api/account/recovery", map[string]any{"email": bad})
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%q was accepted with %d", bad, res.StatusCode)
		}
		res.Body.Close()
	}
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
		{http.MethodPost, "/api/account/password"},
		{http.MethodPost, "/api/account/recovery"},
		{http.MethodPost, "/api/account/recovery/confirm"},
		{http.MethodDelete, "/api/account/recovery"},
	} {
		res := h.do(call.method, call.path, map[string]any{})
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", call.method, call.path, res.StatusCode)
		}
		res.Body.Close()
	}
}
