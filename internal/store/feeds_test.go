package store

import (
	"context"
	"fmt"
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

	if err := s.RecordSuccess(ctx, feed.ID, "The Example", "https://example.com", "", "", 200, 2*time.Hour, next); err != nil {
		t.Fatalf("RecordSuccess(): %v", err)
	}
	got, _ = s.FeedByID(ctx, feed.ID)
	if got.LastError != "" || got.LastErrorBody != "" || got.FailureCount != 0 {
		t.Errorf("a feed that came back still carries %q / %q / %d failures",
			got.LastError, got.LastErrorBody, got.FailureCount)
	}
}

// The cadence is remembered because most fetches cannot recompute it: a publisher answering
// 304 sends no articles, and that is the commonest answer once a feed has been followed for a
// day.
func TestAFeedRemembersHowOftenItIsWorthFetching(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	// Nothing worked out yet, which the poller reads as "a day".
	if feed.FetchInterval != 0 {
		t.Errorf("a new feed already claims an interval of %s", feed.FetchInterval)
	}

	next := s.Now().Add(6 * time.Hour)
	if err := s.RecordSuccess(ctx, feed.ID, "The Example", "", "", "", 200, 6*time.Hour, next); err != nil {
		t.Fatalf("RecordSuccess(): %v", err)
	}

	got, err := s.FeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("FeedByID(): %v", err)
	}
	if got.FetchInterval != 6*time.Hour {
		t.Errorf("interval = %s, want 6h", got.FetchInterval)
	}
}

// Marking a feed read is about what a page has not shown yet as much as what it has: an
// article this person has read is never offered to any of their pages, so this is what makes
// following a feed again after a while start from now rather than from its backlog.
func TestMarkingAFeedReadCoversWhatNoPageHasShown(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	other, err := s.UpsertFeed(ctx, "https://other.example.com/feed.xml", "Another", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}

	now := s.Now()
	ages := []time.Duration{
		2 * time.Hour,       // today
		3 * 24 * time.Hour,  // this week
		10 * 24 * time.Hour, // this month
		60 * 24 * time.Hour, // older
	}
	items := make([]*Item, 0, len(ages)+1)
	for i, age := range ages {
		items = append(items, &Item{
			FeedID: feed.ID, GUID: fmt.Sprintf("g%d", i),
			Title: fmt.Sprintf("Story %d", i), Link: fmt.Sprintf("https://example.com/%d", i),
			PublishedAt: now.Add(-age), FetchedAt: now,
		})
	}
	// One on another feed, which must be left entirely alone.
	items = append(items, &Item{
		FeedID: other.ID, GUID: "elsewhere", Title: "Elsewhere",
		Link: "https://other.example.com/1", PublishedAt: now.Add(-time.Hour), FetchedAt: now,
	})
	if _, err := s.SaveItems(ctx, items); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	// Older than a week: the ten-day and sixty-day ones, and nothing else.
	marked, err := s.MarkFeedRead(ctx, p.ID, feed.ID, now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("MarkFeedRead(): %v", err)
	}
	if marked != 2 {
		t.Errorf("marked %d, want the 2 older than a week", marked)
	}

	read, err := s.ReadArticles(ctx, p.ID)
	if err != nil {
		t.Fatalf("ReadArticles(): %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("%d articles read, want 2", len(read))
	}
	for _, article := range read {
		if article.FeedID != feed.ID {
			t.Errorf("%q is from another feed", article.Title)
		}
	}

	// Everything, which picks up the two that were left.
	if marked, err = s.MarkFeedRead(ctx, p.ID, feed.ID, time.Time{}); err != nil {
		t.Fatalf("MarkFeedRead(): %v", err)
	}
	if marked != 2 {
		t.Errorf("marked %d the second time, want the 2 that were left", marked)
	}
	if read, _ = s.ReadArticles(ctx, p.ID); len(read) != 4 {
		t.Errorf("%d articles read, want all 4 of this feed's", len(read))
	}
}

// A mark somebody made themselves is theirs. Marking a feed read must not move an article they
// finished with last week to the top of Recently read.
func TestMarkingAFeedReadLeavesEarlierMarksAlone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, _ := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	feed, _ := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")

	now := s.Now()
	if _, err := s.SaveItems(ctx, []*Item{{
		FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
		PublishedAt: now.Add(-time.Hour), FetchedAt: now,
	}}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	// Truncated, because read_at is stored in whole seconds and a comparison against a time
	// carrying microseconds fails on the round trip rather than on the behaviour.
	long := now.Add(-30 * 24 * time.Hour).Truncate(time.Second)
	if _, err := s.derived.ExecContext(ctx,
		`INSERT INTO read_articles (principal_id, item_id, feed_id, title, link, published_at, read_at)
		 SELECT ?, id, feed_id, title, link, published_at, ? FROM items WHERE feed_id = ?`,
		p.ID, unix(long), feed.ID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	marked, err := s.MarkFeedRead(ctx, p.ID, feed.ID, time.Time{})
	if err != nil {
		t.Fatalf("MarkFeedRead(): %v", err)
	}
	if marked != 0 {
		t.Errorf("marked %d, want none — it was already read", marked)
	}

	read, _ := s.ReadArticles(ctx, p.ID)
	if len(read) != 1 {
		t.Fatalf("%d read articles, want 1", len(read))
	}
	if !read[0].ReadAt.Equal(long) {
		t.Errorf("read_at moved to %s, want it left at %s", read[0].ReadAt, long)
	}
}

// The inverse: forgetting that a feed was read, so its articles are offered again.
func TestUnmarkingAFeedReadOffersItsArticlesAgain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, _ := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	feed, _ := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	other, _ := s.UpsertFeed(ctx, "https://other.example.com/feed.xml", "Another", "")

	now := s.Now()
	if _, err := s.SaveItems(ctx, []*Item{
		{FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
			PublishedAt: now.Add(-time.Hour), FetchedAt: now},
		{FeedID: feed.ID, GUID: "g2", Title: "Two", Link: "https://example.com/2",
			PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
		{FeedID: other.ID, GUID: "g3", Title: "Elsewhere", Link: "https://other.example.com/1",
			PublishedAt: now.Add(-time.Hour), FetchedAt: now},
	}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	for _, id := range []string{feed.ID, other.ID} {
		if _, err := s.MarkFeedRead(ctx, p.ID, id, time.Time{}); err != nil {
			t.Fatalf("MarkFeedRead(): %v", err)
		}
	}
	if read, _ := s.ReadArticles(ctx, p.ID); len(read) != 3 {
		t.Fatalf("%d read to begin with, want 3", len(read))
	}

	forgotten, err := s.UnmarkFeedRead(ctx, p.ID, feed.ID)
	if err != nil {
		t.Fatalf("UnmarkFeedRead(): %v", err)
	}
	if forgotten != 2 {
		t.Errorf("forgot %d, want this feed's 2", forgotten)
	}

	read, _ := s.ReadArticles(ctx, p.ID)
	if len(read) != 1 {
		t.Fatalf("%d read after, want the other feed's 1", len(read))
	}
	if read[0].FeedID != other.ID {
		t.Errorf("what survived is from %s, want the other feed", read[0].FeedID)
	}

	// And nothing to forget the second time.
	if again, _ := s.UnmarkFeedRead(ctx, p.ID, feed.ID); again != 0 {
		t.Errorf("forgot %d on a feed with nothing marked, want 0", again)
	}
}
