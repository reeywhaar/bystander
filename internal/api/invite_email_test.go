package api

import (
	"errors"
	"net/http"
	"regexp"
	"testing"

	"bystander/internal/store"
)

// The token out of the link a sent invitation carries. It is the only copy: the reply to the
// administrator deliberately has none.
var inviteLink = regexp.MustCompile(`/invite/([A-Za-z0-9_-]+)`)

func tokenFromMail(t *testing.T, body string) string {
	t.Helper()
	found := inviteLink.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("no invitation link in the message:\n%s", body)
	}
	return found[1]
}

// An invitation sent to an address becomes that address's proof.
//
// The link went to that inbox and nowhere else, so whoever accepted it read it — the same
// proof a recovery code gives, out of a mail that had to be sent anyway. The account therefore
// starts with a recovery address rather than being asked to prove the same inbox twice.
func TestAnEmailedInvitationBindsItsAddress(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	out := h.relay()

	var created adminInviteBody
	h.expect(h.do(http.MethodPost, "/api/admin/invites",
		map[string]string{"role": "user", "email": "newcomer@example.com"}),
		http.StatusCreated, &created)

	if created.Email != "newcomer@example.com" {
		t.Errorf("invitation records %q as its address", created.Email)
	}
	// The link is not handed back, and that is the whole of what makes accepting it proof:
	// an administrator who can read it could accept it without ever seeing the address.
	if created.URL != nil {
		t.Errorf("a sent invitation handed the link back as well: %q", *created.URL)
	}

	sent := out.lastTo(t, "newcomer@example.com")
	if sent.To != "newcomer@example.com" {
		t.Fatalf("sent to %q", sent.To)
	}
	token := tokenFromMail(t, sent.Body)

	// The person holding the token is told what their account is about to be attached to.
	var offered inviteBody
	h.expect(h.do(http.MethodGet, "/api/invites/"+token, nil), http.StatusOK, &offered)
	if offered.Email != "newcomer@example.com" {
		t.Errorf("the acceptance page is not told the address: %q", offered.Email)
	}

	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "newcomer", "password": "correct-horse"}),
		http.StatusNoContent, nil)

	p, err := h.store.PrincipalByUsername(t.Context(), "newcomer")
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.store.RecoveryEmail(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	// RecoveryEmail reads the proved table only — an unproved address is not there to read.
	if got != "newcomer@example.com" {
		t.Errorf("recovery address = %q, want the one the invitation was sent to", got)
	}
}

// A link minted to be handed over carries no address, and binds nothing.
func TestAMintedLinkBindsNothing(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	var created adminInviteBody
	h.expect(h.do(http.MethodPost, "/api/admin/invites", map[string]string{"role": "user"}),
		http.StatusCreated, &created)
	if created.URL == nil {
		t.Fatal("a minted link came back without its URL, which is the only time it is readable")
	}
	if created.Email != "" {
		t.Errorf("a minted link claims an address: %q", created.Email)
	}

	token := tokenFromMail(t, *created.URL)
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "newcomer", "password": "correct-horse"}),
		http.StatusNoContent, nil)

	p, err := h.store.PrincipalByUsername(t.Context(), "newcomer")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := h.store.RecoveryEmail(t.Context(), p.ID); err != nil || got != "" {
		t.Errorf("recovery address = %q (%v), want none", got, err)
	}
}

// Refused before anything is written, so no invitation is left outstanding that nobody will
// ever receive. The interface says the same thing rather than greying a button silently.
func TestSendingAnInvitationNeedsARelay(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	before := h.inviteCount()
	h.expect(h.do(http.MethodPost, "/api/admin/invites",
		map[string]string{"role": "user", "email": "newcomer@example.com"}),
		http.StatusConflict, nil)

	if after := h.inviteCount(); after != before {
		t.Errorf("invitations went from %d to %d; a refusal should leave nothing behind",
			before, after)
	}
}

// inviteCount is how many invitations exist. The harness signs in by accepting one of its
// own, so what matters is whether a call added to them.
func (h *harness) inviteCount() int {
	h.t.Helper()
	var listed []adminInviteBody
	h.expect(h.do(http.MethodGet, "/api/admin/invites", nil), http.StatusOK, &listed)
	return len(listed)
}

// A send that fails withdraws the invitation. Left in place it would sit in the list looking
// issued while its only copy of the link is gone, and the way out is minting another anyway.
func TestAnInvitationThatCouldNotBeSentIsWithdrawn(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	out := h.relay()
	out.refuse = errors.New("the relay refused the recipient")

	before := h.inviteCount()
	res := h.do(http.MethodPost, "/api/admin/invites",
		map[string]string{"role": "user", "email": "newcomer@example.com"})
	// A 502, not a 500: everything on this side worked and something upstream did not.
	h.expect(res, http.StatusBadGateway, nil)

	if after := h.inviteCount(); after != before {
		t.Errorf("invitations went from %d to %d; one that was never sent should be withdrawn",
			before, after)
	}
}

// One address belongs to one account, held by whoever proved it last — the same rule as
// proving one with a code, and for the same reason: whoever can read that inbox today is who
// recovery through it would actually reach.
func TestAnEmailedInvitationTakesTheAddressOverFromAnotherAccount(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	out := h.relay()

	// An account that already holds the address, proved the ordinary way.
	first, err := h.store.CreatePrincipal(t.Context(), "bob", "correct-horse", store.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	code, err := h.store.BeginRecovery(t.Context(), first.ID, "shared@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.store.ConfirmRecovery(t.Context(), first.ID, code); err != nil {
		t.Fatal(err)
	}

	var created adminInviteBody
	h.expect(h.do(http.MethodPost, "/api/admin/invites",
		map[string]string{"role": "user", "email": "shared@example.com"}),
		http.StatusCreated, &created)

	token := tokenFromMail(t, out.lastTo(t, "shared@example.com").Body)
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "carol", "password": "correct-horse"}),
		http.StatusNoContent, nil)

	carol, err := h.store.PrincipalByUsername(t.Context(), "carol")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := h.store.RecoveryEmail(t.Context(), carol.ID); got != "shared@example.com" {
		t.Errorf("the new account has %q, want the address it was invited at", got)
	}
	// And the account that had it no longer does — silently, because the only address on file
	// for them is the one they just lost. The takeover is logged instead.
	if got, _ := h.store.RecoveryEmail(t.Context(), first.ID); got != "" {
		t.Errorf("the displaced account still has %q", got)
	}
}

// Plainly not an address is refused before a relay is troubled with it.
func TestAnInvitationToNonsenseIsRefused(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	out := h.relay()

	h.expect(h.do(http.MethodPost, "/api/admin/invites",
		map[string]string{"role": "user", "email": "not-an-address"}),
		http.StatusBadRequest, nil)
	if n := out.count(); n != 0 {
		t.Errorf("%d messages sent for an address that cannot exist", n)
	}
}
