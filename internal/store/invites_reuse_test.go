package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// Two people, one link, at the same moment.
func TestConcurrentAcceptancesOfOneInvite(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatal(err)
	}

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var made []string
	var errs []error

	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, _, err := s.AcceptInvite(ctx, token, fmt.Sprintf("racer%d", i), "correct-horse")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			made = append(made, p.Username)
		}()
	}
	close(start)
	wg.Wait()

	if len(made) != 1 {
		t.Errorf("%d accounts from one invitation: %v", len(made), made)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrConflict) {
			t.Errorf("a loser got %v, want ErrConflict", err)
		}
	}

	// And the database agrees: one account, and the invitation points at it.
	all, err := s.ListPrincipals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("%d principals exist, want the one", len(all))
	}
	inv, err := s.InviteByToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Accepted() || inv.PrincipalID != all[0].ID {
		t.Errorf("the invitation points at %q, want %q", inv.PrincipalID, all[0].ID)
	}
}

// An invitation outlives the account it produced.
//
// The row is the record of where an account came from, which is the one question it exists to
// answer, and it used to be cascaded away with the account — taking the answer with it. The
// column beside it settled the same question the other way already: `created_by` survives its
// issuer as NULL.
//
// The link is still dead, and by the stamp rather than by the row's absence, which is the
// point: what was being lost was the record, never the guarantee.
func TestInviteAfterItsAccountIsDeleted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, token, err := s.CreateInvite(ctx, RoleUser, "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.AcceptInvite(ctx, token, "alice", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePrincipal(ctx, p.ID); err != nil {
		t.Fatal(err)
	}

	inv, err := s.InviteByToken(ctx, token)
	if err != nil {
		t.Fatalf("the invitation went with the account it produced: %v", err)
	}
	if !inv.Accepted() {
		t.Error("the surviving invitation no longer reads as accepted")
	}
	// Nulled, not kept pointing at an account that is gone.
	if inv.PrincipalID != "" {
		t.Errorf("principal_id = %q, want it nulled once the account went", inv.PrincipalID)
	}

	// And the link is still dead — by the stamp it kept, rather than by the row's absence.
	if _, _, err := s.AcceptInvite(ctx, token, "bob", "correct-horse"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second acceptance = %v, want ErrConflict", err)
	}

	all, err := s.ListPrincipals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("%d principals exist, want none", len(all))
	}
}
