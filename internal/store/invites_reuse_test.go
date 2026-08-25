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

// What becomes of an invitation when the account it produced is deleted.
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

	// Does the row survive? docs/entities.md says an accepted invitation keeps its row as
	// the record of where an account came from.
	inv, err := s.InviteByToken(ctx, token)
	t.Logf("after deleting the account: invite=%+v err=%v", inv, err)

	// Whatever the answer, the link must not work again.
	if _, _, err := s.AcceptInvite(ctx, token, "bob", "correct-horse"); err == nil {
		t.Fatal("the link worked again after its account was deleted")
	} else {
		t.Logf("second acceptance refused with: %v", err)
	}

	all, err := s.ListPrincipals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("%d principals exist, want none", len(all))
	}
}
