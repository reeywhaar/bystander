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

	before, err := s.Candidates(ctx, "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Candidates(): %v", err)
	}
	if len(before[feed.ID]) != 1 {
		t.Fatalf("%d articles after the first fetch", len(before[feed.ID]))
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

	after, err := s.Candidates(ctx, "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Candidates(): %v", err)
	}
	if len(after[feed.ID]) != 1 {
		t.Fatalf("%d articles after an edit, want the one", len(after[feed.ID]))
	}
	// The same row, so anything pointing at it still points at it.
	if after[feed.ID][0].ID != before[feed.ID][0].ID {
		t.Errorf("the article changed id: %s then %s", before[feed.ID][0].ID, after[feed.ID][0].ID)
	}
	// And the corrected headline is the one shown, because it is the publisher's latest word.
	if after[feed.ID][0].Title != "Команда Team Spirit выиграла главный мировой турнир по Dota 2" {
		t.Errorf("title = %q", after[feed.ID][0].Title)
	}
	if after[feed.ID][0].PublishedAt.Unix() != published.Unix() {
		t.Errorf("published_at moved to %s; an edit is not a republication", after[feed.ID][0].PublishedAt)
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
	if _, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, nil); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	const link = "https://theblueprint.ru/news/41470"
	first := article(feed.ID, "guid-at-17:48", link, "The first headline", time.Now().Add(-time.Hour))
	if _, err := s.SaveItems(ctx, []*Item{first}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	// It goes on a page, which is what records having been shown.
	if _, err := s.ReplaceEdition(ctx, p.ID, 1, 1,
		[]Pick{{Item: first, Rank: 0, Slot: SlotLead}}); err != nil {
		t.Fatalf("ReplaceEdition(): %v", err)
	}

	seen, err := s.shownHashes(ctx, p.ID, feed.ID)
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
	seen, err = s.shownHashes(ctx, p.ID, feed.ID)
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

	got, err := s.Candidates(ctx, "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatalf("Candidates(): %v", err)
	}
	if len(got[feed.ID]) != 3 {
		t.Errorf("%d articles, want 3: losing articles to prevent duplicates is the wrong way round",
			len(got[feed.ID]))
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
