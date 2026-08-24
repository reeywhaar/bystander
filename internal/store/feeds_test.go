package store

import (
	"context"
	"testing"
	"time"
)

// A failure is stored so it can be explained, and a success wipes it so a feed that came back
// does not keep an old refusal on its record.
func TestAFailureKeepsWhatTheServerSaidAndASuccessClearsIt(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}

	const said = `{"error":"rate limited"}`
	next := s.Now().Add(time.Hour)
	if err := s.RecordFailure(ctx, feed.ID, 429, "the server answered 429 Too Many Requests", said, next); err != nil {
		t.Fatalf("RecordFailure(): %v", err)
	}

	got, err := s.FeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("FeedByID(): %v", err)
	}
	if got.LastStatus != 429 {
		t.Errorf("status = %d, want 429", got.LastStatus)
	}
	if got.LastErrorBody != said {
		t.Errorf("body = %q, want %q", got.LastErrorBody, said)
	}
	if got.FailureCount != 1 {
		t.Errorf("failures = %d, want 1", got.FailureCount)
	}

	// A request that never reached anyone leaves nothing to quote, and the zero status is
	// what the interface reads to say so.
	if err := s.RecordFailure(ctx, feed.ID, 0, "could not reach it", "", next); err != nil {
		t.Fatalf("RecordFailure(): %v", err)
	}
	got, _ = s.FeedByID(ctx, feed.ID)
	if got.LastStatus != 0 || got.LastErrorBody != "" {
		t.Errorf("after an unreachable fetch: status %d, body %q — want neither", got.LastStatus, got.LastErrorBody)
	}

	if err := s.RecordSuccess(ctx, feed.ID, "The Example", "https://example.com", "", "", 200, next); err != nil {
		t.Fatalf("RecordSuccess(): %v", err)
	}
	got, _ = s.FeedByID(ctx, feed.ID)
	if got.LastError != "" || got.LastErrorBody != "" || got.FailureCount != 0 {
		t.Errorf("a feed that came back still carries %q / %q / %d failures",
			got.LastError, got.LastErrorBody, got.FailureCount)
	}
}
