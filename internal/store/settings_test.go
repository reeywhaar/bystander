package store

import (
	"context"
	"testing"
	"time"
)

// Retention used to be a flat thirty days, which quietly made the longer windows a lie: a
// feed set to reach back a year had nothing older than a month to reach into.
func TestRetentionFollowsTheLongestWindow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}

	// Nobody follows anything: the floor decides.
	got, err := s.EffectiveItemRetention(ctx)
	if err != nil {
		t.Fatalf("EffectiveItemRetention(): %v", err)
	}
	if got != MinItemRetention {
		t.Errorf("retention = %s with no feeds, want the floor of %s", got, MinItemRetention)
	}

	daily, err := s.UpsertFeed(ctx, "https://example.com/daily.xml", "Daily", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	slow, err := s.UpsertFeed(ctx, "https://example.com/slow.xml", "Slow", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	first, err := s.Subscribe(ctx, p.ID, daily.ID, DefaultPriority, DefaultArticleWindow, nil)
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	second, err := s.Subscribe(ctx, p.ID, slow.ID, DefaultPriority, DefaultArticleWindow, nil)
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	// Both on the default week: still the floor.
	if got, _ = s.EffectiveItemRetention(ctx); got != MinItemRetention {
		t.Errorf("retention = %s with everything on a week, want %s", got, MinItemRetention)
	}

	// One feed wants a year, so a year is kept — for every feed, because articles are
	// shared between whoever follows them.
	year := 365 * 24 * time.Hour
	if err := s.UpdateSubscription(ctx, p.ID, second.ID, SubscriptionPatch{ArticleWindow: &year}); err != nil {
		t.Fatalf("UpdateSubscription(): %v", err)
	}
	if got, _ = s.EffectiveItemRetention(ctx); got != year {
		t.Errorf("retention = %s with a feed on a year, want %s", got, year)
	}

	// "No limit" takes the ceiling rather than forever: unbounded growth is not a setting
	// anybody meant to choose.
	none := time.Duration(0)
	if err := s.UpdateSubscription(ctx, p.ID, first.ID, SubscriptionPatch{ArticleWindow: &none}); err != nil {
		t.Fatalf("UpdateSubscription(): %v", err)
	}
	if got, _ = s.EffectiveItemRetention(ctx); got != MaxItemRetention {
		t.Errorf("retention = %s with a feed on no limit, want the ceiling of %s", got, MaxItemRetention)
	}
}

func TestTheWindowIsValidated(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	sub, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, DefaultArticleWindow, nil)
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	if sub.ArticleWindow != DefaultArticleWindow {
		t.Errorf("a new feed's window is %s, want %s", sub.ArticleWindow, DefaultArticleWindow)
	}

	odd := 3 * time.Hour
	if err := s.UpdateSubscription(ctx, p.ID, sub.ID, SubscriptionPatch{ArticleWindow: &odd}); err == nil {
		t.Error("UpdateSubscription accepted a window that is not on the list")
	}

	for _, valid := range ArticleWindows {
		window := valid
		if err := s.UpdateSubscription(ctx, p.ID, sub.ID, SubscriptionPatch{ArticleWindow: &window}); err != nil {
			t.Errorf("UpdateSubscription(%s): %v", window, err)
		}
	}
}
