package store

import (
	"context"
	"testing"
	"time"
)

func TestAShareStopsWorkingAfterAWeek(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}

	share, token, err := s.CreateShare(ctx, p.ID, "<opml/>", 3)
	if err != nil {
		t.Fatalf("CreateShare(): %v", err)
	}
	if got, err := s.ShareByToken(ctx, token); err != nil || got.FeedCount != 3 {
		t.Fatalf("ShareByToken() = %v, %v", got, err)
	}

	// A minute before it runs out.
	s.SetClock(func() time.Time { return share.ExpiresAt.Add(-time.Minute) })
	if _, err := s.ShareByToken(ctx, token); err != nil {
		t.Errorf("a link that has not expired was refused: %v", err)
	}

	s.SetClock(func() time.Time { return share.ExpiresAt })
	_, err = s.ShareByToken(ctx, token)
	if err == nil {
		t.Fatal("an expired link still works")
	}
	// The same answer an unknown one gets. Whether a URL was ever real is not a question
	// this should answer to somebody trying enough of them.
	unknown := errString(func() error { _, e := s.ShareByToken(ctx, "not-a-real-token"); return e }())
	if errString(err) == unknown {
		t.Log("expired and unknown are worded alike, as intended")
	}
}

func TestTheTokenIsNotStored(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	_, token, err := s.CreateShare(ctx, p.ID, "<opml/>", 1)
	if err != nil {
		t.Fatalf("CreateShare(): %v", err)
	}

	// The same stance as sessions and invitations: a database file holds nothing that can
	// be replayed against the instance it came from.
	var found int
	if err := s.main.QueryRowContext(ctx,
		`SELECT count(*) FROM shares WHERE hex(token_hash) LIKE ?`, "%"+token+"%").Scan(&found); err != nil {
		t.Fatalf("query: %v", err)
	}
	if found != 0 {
		t.Error("the token itself is in the database")
	}
}

func TestPruningTakesTheDeadLinks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	share, live, err := s.CreateShare(ctx, p.ID, "<opml/>", 1)
	if err != nil {
		t.Fatalf("CreateShare(): %v", err)
	}

	if n, err := s.PruneShares(ctx); err != nil || n != 0 {
		t.Fatalf("PruneShares() took %d live links: %v", n, err)
	}

	s.SetClock(func() time.Time { return share.ExpiresAt.Add(time.Hour) })
	if n, err := s.PruneShares(ctx); err != nil || n != 1 {
		t.Fatalf("PruneShares() = %d, %v; want the expired one gone", n, err)
	}
	if _, err := s.ShareByToken(ctx, live); err == nil {
		t.Error("a pruned link still resolves")
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
