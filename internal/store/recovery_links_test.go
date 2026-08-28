package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// recoverable makes an account with one live session, which is the state every one of these
// tests is about changing or deliberately not changing.
func recoverable(t *testing.T, s *Store, username string) *Principal {
	t.Helper()
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, username, "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if err := s.CreateSession(ctx, "cookie-"+username, p.ID, s.Now().Add(time.Hour), Device{}); err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	return p
}

// The property that makes it safe for an administrator to hand one over unasked.
func TestIssuingARecoveryLinkChangesNothing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	if _, _, err := s.CreateRecoveryLink(ctx, p.ID, ""); err != nil {
		t.Fatalf("CreateRecoveryLink(): %v", err)
	}

	if _, err := s.Authenticate(ctx, "alice", "correct-horse"); err != nil {
		t.Errorf("the password stopped working: %v", err)
	}
	if _, err := s.SessionByToken(ctx, "cookie-alice"); err != nil {
		t.Errorf("the session was ended: %v", err)
	}
}

func TestUsingARecoveryLinkSetsThePasswordAndEndsEverySession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	_, token, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("CreateRecoveryLink(): %v", err)
	}
	if _, err := s.UseRecoveryLink(ctx, token, "battery-staple"); err != nil {
		t.Fatalf("UseRecoveryLink(): %v", err)
	}

	if _, err := s.Authenticate(ctx, "alice", "battery-staple"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	if _, err := s.Authenticate(ctx, "alice", "correct-horse"); err == nil {
		t.Error("the old password still works")
	}
	// The likeliest reason to be here at all is that somebody else holds a session, so
	// leaving them signed in would make the whole exercise decorative.
	if _, err := s.SessionByToken(ctx, "cookie-alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionByToken() = %v, want ErrNotFound", err)
	}

	// The row survives, because "how did somebody get back into this account" is the
	// question it exists to answer.
	after, err := s.RecoveryLinkByToken(ctx, token)
	if err != nil {
		t.Fatalf("RecoveryLinkByToken(): %v", err)
	}
	if !after.Used() {
		t.Error("the link is not marked used")
	}
}

func TestARecoveryLinkIsSingleUse(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	_, token, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("CreateRecoveryLink(): %v", err)
	}
	if _, err := s.UseRecoveryLink(ctx, token, "battery-staple"); err != nil {
		t.Fatalf("first UseRecoveryLink(): %v", err)
	}
	_, err = s.UseRecoveryLink(ctx, token, "something-else-again")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second UseRecoveryLink() = %v, want ErrConflict", err)
	}
}

// From the moment one link is spent, every other outstanding one is indistinguishable from a
// stolen one — and the account has just shown it needs none of them.
func TestUsingOneRecoveryLinkVoidsTheOthers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	_, first, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("first CreateRecoveryLink(): %v", err)
	}
	_, second, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("second CreateRecoveryLink(): %v", err)
	}

	if _, err := s.UseRecoveryLink(ctx, second, "battery-staple"); err != nil {
		t.Fatalf("UseRecoveryLink(): %v", err)
	}

	link, err := s.RecoveryLinkByToken(ctx, first)
	if err != nil {
		t.Fatalf("RecoveryLinkByToken(): %v", err)
	}
	if !link.Voided() {
		t.Error("the older link is still outstanding")
	}
	if _, err := s.UseRecoveryLink(ctx, first, "third-password"); !errors.Is(err, ErrConflict) {
		t.Fatalf("UseRecoveryLink() on the voided link = %v, want ErrConflict", err)
	}

	// Another account's outstanding link is nobody else's business.
	other := recoverable(t, s, "bob")
	_, theirs, err := s.CreateRecoveryLink(ctx, other.ID, "")
	if err != nil {
		t.Fatalf("CreateRecoveryLink() for bob: %v", err)
	}
	if link, err := s.RecoveryLinkByToken(ctx, theirs); err != nil {
		t.Fatalf("RecoveryLinkByToken() for bob: %v", err)
	} else if link.Voided() {
		t.Error("recovering one account voided another account's link")
	}
}

func TestARecoveryLinkExpires(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	_, token, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("CreateRecoveryLink(): %v", err)
	}

	at(t, s, time.Now().Add(RecoveryLinkTTL+time.Minute))
	if _, err := s.UseRecoveryLink(ctx, token, "battery-staple"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UseRecoveryLink() past the expiry = %v, want ErrInvalid", err)
	}
	if _, err := s.Authenticate(ctx, "alice", "correct-horse"); err != nil {
		t.Errorf("the old password stopped working anyway: %v", err)
	}
}

func TestAnUnknownRecoveryTokenIsNotFound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.RecoveryLinkByToken(ctx, "not-a-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RecoveryLinkByToken() = %v, want ErrNotFound", err)
	}
	if _, err := s.UseRecoveryLink(ctx, "not-a-token", "battery-staple"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UseRecoveryLink() = %v, want ErrNotFound", err)
	}
}

// A new password would not let anybody into a disabled account, so saying so is better than
// letting somebody set one and find out at the login form.
func TestARecoveryLinkIsRefusedForADisabledAccount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	_, token, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("CreateRecoveryLink(): %v", err)
	}
	if err := s.SetDisabled(ctx, p.ID, true); err != nil {
		t.Fatalf("SetDisabled(): %v", err)
	}

	// Refused at both ends: an account can be switched off in the day between a link being
	// handed out and being opened.
	if _, _, err := s.CreateRecoveryLink(ctx, p.ID, ""); !errors.Is(err, ErrConflict) {
		t.Errorf("CreateRecoveryLink() = %v, want ErrConflict", err)
	}
	if _, err := s.UseRecoveryLink(ctx, token, "battery-staple"); !errors.Is(err, ErrConflict) {
		t.Errorf("UseRecoveryLink() = %v, want ErrConflict", err)
	}
}

func TestARecoveryLinkRefusesAShortPassword(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	_, token, err := s.CreateRecoveryLink(ctx, p.ID, "")
	if err != nil {
		t.Fatalf("CreateRecoveryLink(): %v", err)
	}
	if _, err := s.UseRecoveryLink(ctx, token, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UseRecoveryLink() = %v, want ErrInvalid", err)
	}
	// And the link is still there to try again with, because nothing was spent.
	if link, err := s.RecoveryLinkByToken(ctx, token); err != nil {
		t.Fatalf("RecoveryLinkByToken(): %v", err)
	} else if !link.Usable(s.Now()) {
		t.Error("a refused password spent the link")
	}
}

// The lookup behind the forgotten-password form. Only the proved address counts: one somebody
// is partway through proving is one they merely typed.
func TestPrincipalByRecoveryEmail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := recoverable(t, s, "alice")

	if _, err := s.PrincipalByRecoveryEmail(ctx, "alice@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PrincipalByRecoveryEmail() before anything = %v, want ErrNotFound", err)
	}

	code, err := s.BeginRecovery(ctx, p.ID, "alice@example.com")
	if err != nil {
		t.Fatalf("BeginRecovery(): %v", err)
	}
	if _, err := s.PrincipalByRecoveryEmail(ctx, "alice@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PrincipalByRecoveryEmail() on a pending address = %v, want ErrNotFound", err)
	}

	if _, _, err := s.ConfirmRecovery(ctx, p.ID, code); err != nil {
		t.Fatalf("ConfirmRecovery(): %v", err)
	}
	found, err := s.PrincipalByRecoveryEmail(ctx, "ALICE@Example.com")
	if err != nil {
		t.Fatalf("PrincipalByRecoveryEmail(): %v", err)
	}
	if found.ID != p.ID {
		t.Errorf("found %q, want %q", found.ID, p.ID)
	}
}
