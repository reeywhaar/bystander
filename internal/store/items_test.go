package store

import (
	"context"
	"testing"
	"time"

	"bystander/internal/ids"
)

// article is one item as a fetch would hand it over.
func article(feedID, guid, link, title string, published time.Time) *Item {
	return &Item{
		ID:          ids.New(ids.Article),
		FeedID:      feedID,
		GUID:        guid,
		Title:       title,
		Link:        link,
		Summary:     "<p>A summary.</p>",
		PublishedAt: published,
		FetchedAt:   time.Now(),
	}
}

// The bug this exists for, in the shape it actually arrived in.
//
// theblueprint.ru writes <guid>https://…/post/view?id=41470 Sun, 23 Aug 2026 17:48:00
// +0300</guid> — the permalink with the publication time stuck on the end. Editing the
// headline moved the time, which moved the guid, and the same story appeared on the page
// twice with the old headline beside the new one.
func TestAnArticleWhoseGuidMovedIsStillTheSameArticle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://theblueprint.ru/rss", "The Blueprint", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	const link = "https://theblueprint.ru/news/41470"
	published := time.Now().Add(-2 * time.Hour).Truncate(time.Second)

	added, err := s.SaveItems(ctx, []*Item{article(feed.ID,
		"https://theblueprint.ru/post/view?id=41470 Sun, 23 Aug 2026 17:48:00 +0300",
		link, "Российская киберспортивная команда Team Spirit выиграла главный турнир", published)})
	if err != nil || added != 1 {
		t.Fatalf("SaveItems() = %d, %v", added, err)
	}

	before, err := s.Queues(ctx, "", "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	if len(before[feed.ID].Fresh) != 1 {
		t.Fatalf("%d articles after the first fetch", len(before[feed.ID].Fresh))
	}

	// The publisher shortens the headline. Same story, same link, and a guid that has moved
	// because the time inside it did.
	added, err = s.SaveItems(ctx, []*Item{article(feed.ID,
		"https://theblueprint.ru/post/view?id=41470 Sun, 23 Aug 2026 17:55:12 +0300",
		link, "Команда Team Spirit выиграла главный мировой турнир по Dota 2", published)})
	if err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	if added != 0 {
		t.Errorf("an edited article counted as %d new ones", added)
	}

	after, err := s.Queues(ctx, "", "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	if len(after[feed.ID].Fresh) != 1 {
		t.Fatalf("%d articles after an edit, want the one", len(after[feed.ID].Fresh))
	}
	// The same row, so anything pointing at it still points at it.
	if after[feed.ID].Fresh[0].ID != before[feed.ID].Fresh[0].ID {
		t.Errorf("the article changed id: %s then %s", before[feed.ID].Fresh[0].ID, after[feed.ID].Fresh[0].ID)
	}
	// And the corrected headline is the one shown, because it is the publisher's latest word.
	if after[feed.ID].Fresh[0].Title != "Команда Team Spirit выиграла главный мировой турнир по Dota 2" {
		t.Errorf("title = %q", after[feed.ID].Fresh[0].Title)
	}
	if after[feed.ID].Fresh[0].PublishedAt.Unix() != published.Unix() {
		t.Errorf("published_at moved to %s; an edit is not a republication", after[feed.ID].Fresh[0].PublishedAt)
	}
}

// The other half of the same bug: an article somebody has already been shown must not come
// back as new just because its publisher edited it.
func TestAnEditedArticleIsNotShownAgain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	feed, err := s.UpsertFeed(ctx, "https://theblueprint.ru/rss", "The Blueprint", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if _, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, DefaultArticleWindow, nil); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	const link = "https://theblueprint.ru/news/41470"
	first := article(feed.ID, "guid-at-17:48", link, "The first headline", time.Now().Add(-time.Hour))
	if _, err := s.SaveItems(ctx, []*Item{first}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	// It goes on a page, which is what records having been shown.
	page, err := s.PageByID(ctx, MainPageID(p.ID))
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}
	if _, err := s.AddEdition(ctx, page, 1,
		[]Pick{{Item: first, Rank: 0, Slot: SlotLead}}); err != nil {
		t.Fatalf("AddEdition(): %v", err)
	}

	seen, err := s.shownHashes(ctx, page.ID, feed.ID)
	if err != nil {
		t.Fatalf("shownHashes(): %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("%d shown records, want one", len(seen))
	}

	// Now the edit.
	if _, err := s.SaveItems(ctx, []*Item{article(feed.ID,
		"guid-at-17:55", link, "The corrected headline", time.Now().Add(-time.Hour))}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	// The record of having shown it is kept against the guid, so a rename has to carry it
	// across. Otherwise the article is handed back to the sampler as something nobody has
	// seen — which is the duplicate again, one cycle later.
	seen, err = s.shownHashes(ctx, page.ID, feed.ID)
	if err != nil {
		t.Fatalf("shownHashes(): %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("%d shown records after an edit, want one", len(seen))
	}
	if !seen[string(GUIDHash("guid-at-17:55"))] {
		t.Error("the article reads as never shown under its new guid")
	}

	// And what was read keeps up. Its copy of the headline is there to outlive the article,
	// not to disagree with it while it is still around.
	if err := s.SetRead(ctx, p.ID, first.ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}
	if _, err := s.SaveItems(ctx, []*Item{article(feed.ID,
		"guid-at-18:02", link, "The third headline", time.Now().Add(-time.Hour))}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	read, err := s.ReadArticles(ctx, p.ID)
	if err != nil {
		t.Fatalf("ReadArticles(): %v", err)
	}
	if len(read) != 1 {
		t.Fatalf("%d read articles, want one", len(read))
	}
	if read[0].Title != "The third headline" {
		t.Errorf("read article title = %q, want the corrected one", read[0].Title)
	}
}

// The guard. A feed whose items all point at one page is a real thing, and matching those on
// their link would fold the whole feed into a single article.
func TestArticlesSharingOneLinkAreLeftAlone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "Everything At Once", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	const home = "https://example.com/"
	now := time.Now()

	added, err := s.SaveItems(ctx, []*Item{
		article(feed.ID, "a", home, "First", now.Add(-3*time.Hour)),
		article(feed.ID, "b", home, "Second", now.Add(-2*time.Hour)),
		article(feed.ID, "c", home, "Third", now.Add(-time.Hour)),
	})
	if err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	if added != 3 {
		t.Fatalf("added = %d, want all three kept", added)
	}

	got, err := s.Queues(ctx, "", "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	if len(got[feed.ID].Fresh) != 3 {
		t.Errorf("%d articles, want 3: losing articles to prevent duplicates is the wrong way round",
			len(got[feed.ID].Fresh))
	}
}

// Re-fetching an unchanged feed still has to be free.
func TestRefetchingAnUnchangedFeedAddsNothing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	now := time.Now()
	batch := func() []*Item {
		return []*Item{
			article(feed.ID, "guid-1", "https://example.com/1", "One", now.Add(-2*time.Hour)),
			article(feed.ID, "guid-2", "https://example.com/2", "Two", now.Add(-time.Hour)),
		}
	}

	if added, err := s.SaveItems(ctx, batch()); err != nil || added != 2 {
		t.Fatalf("first SaveItems() = %d, %v", added, err)
	}
	if added, err := s.SaveItems(ctx, batch()); err != nil || added != 0 {
		t.Fatalf("second SaveItems() = %d, %v; a re-fetch is not news", added, err)
	}
}

// The same article keeps the same id, whoever saved it and whenever.
func TestAnArticleKeepsItsIdAcrossFetches(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	now := time.Now()
	one := func() *Item {
		return article(feed.ID, "guid-1", "https://example.com/1", "One", now.Add(-time.Hour))
	}

	first := one()
	if _, err := s.SaveItems(ctx, []*Item{first}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	// The id in the table, not the one the caller happened to arrive with.
	if first.ID != ids.Derive(ids.Article, feed.ID, "guid-1") {
		t.Errorf("id = %q, want one derived from the feed and the guid", first.ID)
	}

	second := one()
	if _, err := s.SaveItems(ctx, []*Item{second}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("a re-fetch renamed the article: %s then %s", first.ID, second.ID)
	}

	// Including after it has been pruned and the feed still lists it. Read marks and shown
	// records outlive an article by design, and a new id would have wasted both.
	if _, err := s.Derived().ExecContext(ctx, `DELETE FROM items WHERE id = ?`, first.ID); err != nil {
		t.Fatalf("prune: %v", err)
	}
	third := one()
	if _, err := s.SaveItems(ctx, []*Item{third}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	if third.ID != first.ID {
		t.Errorf("an article that came back got a new id: %s then %s", first.ID, third.ID)
	}
}

// Two feeds carrying the same article are still two articles.
//
// The regression this exists for: articles were named while parsing, and discovery parses
// before there is a feed to name them after. Every feed's articles came out identically
// named, and the second feed's were dropped by the primary key one at a time.
func TestTwoFeedsWithTheSameArticleKeepBoth(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	one, err := s.UpsertFeed(ctx, "https://one.example/feed.xml", "One", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	two, err := s.UpsertFeed(ctx, "https://two.example/feed.xml", "Two", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}

	now := time.Now().Add(-time.Hour)
	syndicated := func(feedID string) *Item {
		return article(feedID, "https://wire.example/story-1", "https://wire.example/story-1",
			"The same story, carried twice", now)
	}

	added, err := s.SaveItems(ctx, []*Item{syndicated(one.ID)})
	if err != nil || added != 1 {
		t.Fatalf("SaveItems() = %d, %v", added, err)
	}
	added, err = s.SaveItems(ctx, []*Item{syndicated(two.ID)})
	if err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	if added != 1 {
		t.Fatalf("the second feed's copy was dropped: added = %d", added)
	}

	got, err := s.Queues(ctx, "", "", []string{one.ID, two.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	if len(got[one.ID].Fresh) != 1 || len(got[two.ID].Fresh) != 1 {
		t.Errorf("articles per feed = %d and %d, want one each",
			len(got[one.ID].Fresh), len(got[two.ID].Fresh))
	}
}

// What somebody has read outlives any retention, because it has a job that does not expire.
//
// It was kept for a month and dropped. That was right while its only job was a list to look
// back at; it now also decides what a page may draw from — an article this person has read is
// not offered to any of their pages again — and a memory that forgets after a month hands back
// a story a year later as though it were new.
func TestWhatWasReadIsKeptHoweverLongAgoItWas(t *testing.T) {
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

	// Read two years ago, which is well past any retention this ever had.
	long := s.Now().Add(-2 * 365 * 24 * time.Hour)
	if _, err := s.derived.ExecContext(ctx,
		`INSERT INTO read_articles (principal_id, item_id, feed_id, title, link, published_at, read_at)
		 VALUES (?, 'a_old', ?, 'An old headline', 'https://example.com/old', ?, ?)`,
		p.ID, feed.ID, unix(long), unix(long)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	read, err := s.ReadArticles(ctx, p.ID)
	if err != nil {
		t.Fatalf("ReadArticles(): %v", err)
	}
	if len(read) != 1 {
		t.Fatalf("%d articles in the list, want the one read two years ago", len(read))
	}

	// And the sweep leaves it, because the feed is still followed.
	if n, err := s.PruneReadArticles(ctx, []string{feed.ID}); err != nil || n != 0 {
		t.Errorf("PruneReadArticles() collected %d, %v — want it left alone", n, err)
	}
}

// Unfollowing a feed takes what was read there with it: the record's job is to keep an article
// somebody has finished with off their pages, and a feed they no longer follow puts nothing on
// one. Following it again is a fresh start.
func TestUnfollowingAFeedForgetsWhatWasReadThere(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	kept, err := s.UpsertFeed(ctx, "https://kept.example.com/feed.xml", "Kept", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	dropped, err := s.UpsertFeed(ctx, "https://dropped.example.com/feed.xml", "Dropped", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}

	var subs []*Subscription
	for _, feed := range []*Feed{kept, dropped} {
		sub, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, DefaultArticleWindow, nil)
		if err != nil {
			t.Fatalf("Subscribe(): %v", err)
		}
		subs = append(subs, sub)
		if _, err := s.derived.ExecContext(ctx,
			`INSERT INTO read_articles (principal_id, item_id, feed_id, title, link, published_at, read_at)
			 VALUES (?, ?, ?, 'A headline', 'https://example.com/1', 100, 200)`,
			p.ID, "a_"+feed.ID, feed.ID); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := s.DeleteSubscription(ctx, p.ID, subs[1].ID); err != nil {
		t.Fatalf("DeleteSubscription(): %v", err)
	}

	read, err := s.ReadArticles(ctx, p.ID)
	if err != nil {
		t.Fatalf("ReadArticles(): %v", err)
	}
	if len(read) != 1 {
		t.Fatalf("%d articles left, want only the one on the feed still followed", len(read))
	}
	if read[0].FeedID != kept.ID {
		t.Errorf("what was left is from %s, want the feed still followed", read[0].FeedID)
	}
}

// One person unfollowing must not forget what another person read on the same feed.
func TestUnfollowingLeavesEverybodyElsesReadingAlone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}

	var leaving *Subscription
	var people []*Principal
	for _, name := range []string{"alice", "bob"} {
		p, err := s.CreatePrincipal(ctx, name, "correct-horse", RoleUser)
		if err != nil {
			t.Fatalf("CreatePrincipal(): %v", err)
		}
		people = append(people, p)
		sub, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, DefaultArticleWindow, nil)
		if err != nil {
			t.Fatalf("Subscribe(): %v", err)
		}
		if name == "alice" {
			leaving = sub
		}
		if _, err := s.derived.ExecContext(ctx,
			`INSERT INTO read_articles (principal_id, item_id, feed_id, title, link, published_at, read_at)
			 VALUES (?, 'a_1', ?, 'A headline', 'https://example.com/1', 100, 200)`,
			p.ID, feed.ID); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := s.DeleteSubscription(ctx, people[0].ID, leaving.ID); err != nil {
		t.Fatalf("DeleteSubscription(): %v", err)
	}

	if read, _ := s.ReadArticles(ctx, people[0].ID); len(read) != 0 {
		t.Errorf("the person who unfollowed still has %d read articles", len(read))
	}
	if read, _ := s.ReadArticles(ctx, people[1].ID); len(read) != 1 {
		t.Errorf("the person still following has %d, want theirs untouched", len(read))
	}
}
