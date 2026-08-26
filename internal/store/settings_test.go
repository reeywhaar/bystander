package store

import (
	"context"
	"testing"
	"time"
)

// One number for the whole instance was one number too few: it took the longest window
// chosen anywhere, so a webcomic somebody wanted a year of made a news feed at ninety
// articles a day keep a year as well.
func TestRetentionIsPerFeed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	news, err := s.UpsertFeed(ctx, "https://example.com/news.xml", "News", "")
	if err != nil {
		t.Fatal(err)
	}
	comic, err := s.UpsertFeed(ctx, "https://example.com/comic.xml", "Comic", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(ctx, p.ID, news.ID, DefaultPriority, 7*24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(ctx, p.ID, comic.ID, DefaultPriority, 0, nil); err != nil {
		t.Fatal(err)
	}

	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatalf("ItemRetentionByFeed(): %v", err)
	}
	// A week is under the floor, so the news feed gets the floor — and nothing near the
	// comic's answer.
	if got := byFeed[news.ID]; got.Forever || got.For != MinItemRetention {
		t.Errorf("the news feed keeps %+v, want the floor of %s", got, MinItemRetention)
	}
	if got := byFeed[comic.ID]; !got.Forever {
		t.Errorf("the comic keeps %+v, want forever", got)
	}

	// The most demanding follower of a feed wins, and only for that feed.
	bob, err := s.CreatePrincipal(ctx, "bob", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	year := 365 * 24 * time.Hour
	if _, err := s.Subscribe(ctx, bob.ID, news.ID, DefaultPriority, year, nil); err != nil {
		t.Fatal(err)
	}
	if byFeed, err = s.ItemRetentionByFeed(ctx); err != nil {
		t.Fatal(err)
	}
	if got := byFeed[news.ID]; got.Forever || got.For != year {
		t.Errorf("the news feed keeps %+v, want a year now somebody asked for one", got)
	}

	// A feed nobody follows is absent rather than zero: it constrains nothing, and
	// everything it has is due to be collected.
	if _, ok := byFeed["f_nobody"]; ok {
		t.Error("a feed nobody follows has an opinion about retention")
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
