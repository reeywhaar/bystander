package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAcceptInvite(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	inv, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}

	p, _, err := s.AcceptInvite(ctx, token, "alice", "correct-horse")
	if err != nil {
		t.Fatalf("AcceptInvite(): %v", err)
	}
	if p.Role != RoleUser {
		t.Errorf("role = %q, want user", p.Role)
	}

	// The row survives acceptance and points at what it produced: that is the record of
	// where the account came from.
	after, err := s.InviteByToken(ctx, token)
	if err != nil {
		t.Fatalf("InviteByToken(): %v", err)
	}
	if !after.Accepted() {
		t.Error("the invitation is not marked accepted")
	}
	if after.PrincipalID != p.ID {
		t.Errorf("invite points at %q, want %q", after.PrincipalID, p.ID)
	}
	if after.ID != inv.ID {
		t.Errorf("invite id changed: %q became %q", inv.ID, after.ID)
	}
}

func TestAnInviteIsSingleUse(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}
	if _, _, err := s.AcceptInvite(ctx, token, "alice", "correct-horse"); err != nil {
		t.Fatalf("first AcceptInvite(): %v", err)
	}
	_, _, err = s.AcceptInvite(ctx, token, "bob", "correct-horse")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second AcceptInvite() = %v, want ErrConflict", err)
	}
}

// A rejected acceptance must leave nothing behind: no account, no stamp on the invitation.
func TestAcceptInviteRollsBack(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser); err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	_, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}

	if _, _, err := s.AcceptInvite(ctx, token, "alice", "another-password"); !errors.Is(err, ErrConflict) {
		t.Fatalf("AcceptInvite() with a taken name = %v, want ErrConflict", err)
	}

	inv, err := s.InviteByToken(ctx, token)
	if err != nil {
		t.Fatalf("InviteByToken(): %v", err)
	}
	if inv.Accepted() {
		t.Error("a failed acceptance still stamped the invitation")
	}
}

func TestExpiredInvite(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	start := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	at(t, s, start)

	_, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}

	at(t, s, start.Add(InviteTTL+time.Second))
	if _, _, err := s.AcceptInvite(ctx, token, "alice", "correct-horse"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AcceptInvite() past expiry = %v, want ErrInvalid", err)
	}

	// It is still readable, so the acceptance page can say "expired" rather than
	// "no such link" — three states a person can act on differently.
	inv, err := s.InviteByToken(ctx, token)
	if err != nil {
		t.Fatalf("InviteByToken(): %v", err)
	}
	if inv.Usable(s.Now()) {
		t.Error("an expired invitation reports itself usable")
	}
}

func TestDeleteAcceptedInviteIsRefused(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	inv, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}
	if _, _, err := s.AcceptInvite(ctx, token, "alice", "correct-horse"); err != nil {
		t.Fatalf("AcceptInvite(): %v", err)
	}
	if err := s.DeleteInvite(ctx, inv.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("DeleteInvite() on an accepted invitation = %v, want ErrConflict", err)
	}
}

func TestUnknownTokenIsNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.InviteByToken(context.Background(), "nonsense"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("InviteByToken(unknown) = %v, want ErrNotFound", err)
	}
}
