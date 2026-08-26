package store

import (
	"context"
	"testing"
	"time"
)

// Retention used to be a flat thirty days, which quietly made the longer windows a lie: a
// feed set to reach back a year had nothing older than a month to reach into.
//
// This is the one number the *shown* record uses, which has to outlive every article
// whichever feed it came from. What is actually pruned goes feed by feed —
// see TestRetentionIsPerFeed.
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
	if got.Forever || got.For != MinItemRetention {
		t.Errorf("retention = %+v with no feeds, want the floor of %s", got, MinItemRetention)
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
	if got, _ = s.EffectiveItemRetention(ctx); got.Forever || got.For != MinItemRetention {
		t.Errorf("retention = %+v with everything on a week, want %s", got, MinItemRetention)
	}

	// One feed wants a year, so a year is kept — for every feed, because articles are
	// shared between whoever follows them.
	year := 365 * 24 * time.Hour
	if err := s.UpdateSubscription(ctx, p.ID, second.ID, SubscriptionPatch{ArticleWindow: &year}); err != nil {
		t.Fatalf("UpdateSubscription(): %v", err)
	}
	if got, _ = s.EffectiveItemRetention(ctx); got.Forever || got.For != year {
		t.Errorf("retention = %+v with a feed on a year, want %s", got, year)
	}

	// "No limit" means no limit. It used to take a one-year ceiling, and that was wrong for
	// a reason worth writing down: how far back a page reaches bounds when an article was
	// *published*, and pruning goes by when it was *fetched*. A feed whose every article
	// was written two years ago — an archive, a comic's back catalogue, a blog that stopped
	// — would have had its articles dropped a year after they were first seen, and if the
	// publisher had moved them out of the document by then they would never have come back.
	// What bounds such a feed is MaxItemsPerFeed, which is a shelf length rather than a date.
	none := time.Duration(0)
	if err := s.UpdateSubscription(ctx, p.ID, first.ID, SubscriptionPatch{ArticleWindow: &none}); err != nil {
		t.Fatalf("UpdateSubscription(): %v", err)
	}
	if got, _ = s.EffectiveItemRetention(ctx); !got.Forever {
		t.Errorf("retention = %+v with a feed on no limit, want forever", got)
	}
}

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
