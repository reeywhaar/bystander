package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// savedPage is the page of saved articles, or nil.
func savedPage(t *testing.T, s *Store, principalID string) *Page {
	t.Helper()
	pages, err := s.Pages(context.Background(), principalID)
	if err != nil {
		t.Fatalf("Pages(): %v", err)
	}
	var found *Page
	for _, page := range pages {
		if page.IsSaved {
			if found != nil {
				t.Fatalf("two pages of saved articles: %s and %s", found.ID, page.ID)
			}
			found = page
		}
	}
	return found
}

func TestTheFirstSaveMakesThePageOfSavedArticles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)
	feedID := seedFeed(t, s, p.ID, "meridian", 0, 1, 2)

	// The address it would take is already somebody's page.
	if _, err := s.CreatePage(ctx, p.ID, "Later", "later"); err != nil {
		t.Fatalf("CreatePage(): %v", err)
	}
	if savedPage(t, s, p.ID) != nil {
		t.Fatal("a page of saved articles before anything was saved")
	}

	queues, err := s.Queues(ctx, MainPageID(p.ID), p.ID, []string{feedID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	for _, item := range queues[feedID].Fresh {
		if err := s.SaveArticle(ctx, p.ID, item.ID); err != nil {
			t.Fatalf("SaveArticle(): %v", err)
		}
	}

	page := savedPage(t, s, p.ID)
	if page == nil {
		t.Fatal("no page of saved articles after saving")
	}
	if page.Name != SavedPageName || page.Slug != "later-2" {
		t.Errorf("page is %q at %q, want %q at %q", page.Name, page.Slug, SavedPageName, "later-2")
	}
	if page.NextEditionAt.After(s.Now()) {
		t.Errorf("due %v, want now so the first save is on a page by the next tick", page.NextEditionAt)
	}
}

// A saved article is a copy, and it has to outlive everything the article itself does not:
// its feed being unfollowed, its row being pruned, and the read mark that goes with the feed.
func TestASavedArticleOutlivesItsFeed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)
	saving := time.Now().UTC().Truncate(time.Second)
	at(t, s, saving)

	feed, err := s.UpsertFeed(ctx, "https://example.com/meridian.xml", "The Meridian", "https://example.com")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	sub, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, 0, nil)
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	item := &Item{FeedID: feed.ID, GUID: "crossing", Title: "The crossing that reopened",
		Link: "https://example.com/crossing", PublishedAt: s.Now(), FetchedAt: s.Now()}
	if _, err := s.SaveItems(ctx, []*Item{item}); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	if err := s.SaveArticle(ctx, p.ID, item.ID); err != nil {
		t.Fatalf("SaveArticle(): %v", err)
	}
	// Read later, on the saved page — which is the read that counts there.
	at(t, s, saving.Add(time.Hour))
	if err := s.SetRead(ctx, p.ID, item.ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}

	// Unfollowed, and then everything the sweep does to a feed nobody follows.
	if err := s.DeleteSubscription(ctx, p.ID, sub.ID); err != nil {
		t.Fatalf("DeleteSubscription(): %v", err)
	}
	if _, err := s.PruneItems(ctx, nil); err != nil {
		t.Fatalf("PruneItems(): %v", err)
	}
	if _, err := s.PruneReadArticles(ctx, nil); err != nil {
		t.Fatalf("PruneReadArticles(): %v", err)
	}
	if _, err := s.ItemByID(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the article itself survived the sweep (%v), so this tests nothing", err)
	}

	page := savedPage(t, s, p.ID)
	queues, err := s.SavedQueues(ctx, page.ID, p.ID)
	if err != nil {
		t.Fatalf("SavedQueues(): %v", err)
	}
	q := queues[item.ID]
	if q == nil || len(q.Read) != 1 {
		t.Fatalf("queue %+v, want the article in the read band: the read mark went with the feed", q)
	}
	if got := q.Read[0].Title; got != item.Title {
		t.Errorf("title %q, want the saved copy's %q", got, item.Title)
	}

	// Composing puts the article back, so the edition has a row to point at.
	if _, err := s.AddEdition(ctx, page, 1, []Pick{{Item: q.Read[0], Slot: SlotLead}}); err != nil {
		t.Fatalf("AddEdition(): %v", err)
	}
	if _, err := s.ItemByID(ctx, item.ID); err != nil {
		t.Errorf("ItemByID() after composing: %v", err)
	}

	saved, err := s.SavedAmong(ctx, p.ID, []string{item.ID})
	if err != nil {
		t.Fatalf("SavedAmong(): %v", err)
	}
	if got := saved[item.ID]; got == nil || got.SourceTitle != "The Meridian" {
		t.Errorf("SavedAmong() = %+v, want the source kept as The Meridian", got)
	}
}

// What the page has shown is a band, as on any other page: a saved article not yet shown comes
// before one shown and left unread.
func TestSavedArticlesAreBandedLikeAnyOther(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)
	saving := time.Now().UTC().Truncate(time.Second)
	at(t, s, saving)
	feedID := seedFeed(t, s, p.ID, "meridian", 0, 1, 2, 3)

	queues, err := s.Queues(ctx, MainPageID(p.ID), p.ID, []string{feedID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	items := queues[feedID].Fresh
	for _, item := range items {
		if err := s.SaveArticle(ctx, p.ID, item.ID); err != nil {
			t.Fatalf("SaveArticle(): %v", err)
		}
	}
	page := savedPage(t, s, p.ID)

	shown, read, fresh := items[0], items[1], items[2]
	if _, err := s.AddEdition(ctx, page, 1, []Pick{{Item: shown, Slot: SlotLead}}); err != nil {
		t.Fatalf("AddEdition(): %v", err)
	}
	at(t, s, saving.Add(time.Hour))
	if err := s.SetRead(ctx, p.ID, read.ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}

	got, err := s.SavedQueues(ctx, page.ID, p.ID)
	if err != nil {
		t.Fatalf("SavedQueues(): %v", err)
	}
	for _, want := range []struct {
		item *Item
		band func(*Queue) []*Item
		name string
	}{
		{shown, func(q *Queue) []*Item { return q.Unread }, "unread"},
		{read, func(q *Queue) []*Item { return q.Read }, "read"},
		{fresh, func(q *Queue) []*Item { return q.Fresh }, "fresh"},
	} {
		if q := got[want.item.ID]; q == nil || len(want.band(q)) != 1 {
			t.Errorf("%s: queue %+v, want it in the %s band", want.item.GUID, q, want.name)
		}
	}

	if err := s.UnsaveArticle(ctx, p.ID, fresh.ID); err != nil {
		t.Fatalf("UnsaveArticle(): %v", err)
	}
	if err := s.UnsaveArticle(ctx, p.ID, fresh.ID); err != nil {
		t.Errorf("UnsaveArticle() twice: %v, want nothing to complain about", err)
	}
	got, err = s.SavedQueues(ctx, page.ID, p.ID)
	if err != nil {
		t.Fatalf("SavedQueues(): %v", err)
	}
	if _, still := got[fresh.ID]; still || len(got) != 2 {
		t.Errorf("%d queues after unsaving one of three, want 2", len(got))
	}
}

// Saving puts an article aside, which is dealing with it on the page it was found on: it greys
// there and everywhere else. Not on the saved page, where it has only just arrived — and not again
// on a second save, which would otherwise stamp a read later than the save.
func TestSavingMarksReadEverywhereButTheSavedPage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)
	saving := time.Now().UTC().Truncate(time.Second)
	at(t, s, saving)
	feedID := seedFeed(t, s, p.ID, "meridian", 0, 1)

	queues, err := s.Queues(ctx, MainPageID(p.ID), p.ID, []string{feedID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	item := queues[feedID].Fresh[0]
	if err := s.SaveArticle(ctx, p.ID, item.ID); err != nil {
		t.Fatalf("SaveArticle(): %v", err)
	}

	queues, err = s.Queues(ctx, MainPageID(p.ID), p.ID, []string{feedID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	if len(queues[feedID].Read) != 1 {
		t.Errorf("Front Page queue %+v, want the saved article read there", queues[feedID])
	}

	at(t, s, saving.Add(time.Hour))
	if err := s.SaveArticle(ctx, p.ID, item.ID); err != nil {
		t.Fatalf("SaveArticle() again: %v", err)
	}

	saved, err := s.SavedQueues(ctx, savedPage(t, s, p.ID).ID, p.ID)
	if err != nil {
		t.Fatalf("SavedQueues(): %v", err)
	}
	if q := saved[item.ID]; q == nil || len(q.Fresh) != 1 {
		t.Errorf("saved page queue %+v, want it fresh: the read was the save's own", q)
	}
}

func TestThePageOfSavedArticlesTakesNoFilter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)
	feedID := seedFeed(t, s, p.ID, "meridian", 0, 1)

	queues, err := s.Queues(ctx, MainPageID(p.ID), p.ID, []string{feedID}, 10, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	if err := s.SaveArticle(ctx, p.ID, queues[feedID].Fresh[0].ID); err != nil {
		t.Fatalf("SaveArticle(): %v", err)
	}
	page := savedPage(t, s, p.ID)

	week := 7 * 24 * time.Hour
	for name, patch := range map[string]PagePatch{
		"a feed":   {IncludeFeedIDs: []string{feedID}},
		"a window": {ArticleWindow: &week},
	} {
		if err := s.UpdatePage(ctx, page.ID, patch); !errors.Is(err, ErrInvalid) {
			t.Errorf("UpdatePage() with %s = %v, want ErrInvalid", name, err)
		}
	}

	// What the interface sends when it saves the page as it is: empty lists and no window.
	none := time.Duration(0)
	size := 20
	if err := s.UpdatePage(ctx, page.ID, PagePatch{
		EditionSize: &size, ArticleWindow: &none,
		IncludeTagIDs: []string{}, ExcludeTagIDs: []string{},
		IncludeFeedIDs: []string{}, ExcludeFeedIDs: []string{},
	}); err != nil {
		t.Errorf("UpdatePage() with nothing narrowed: %v", err)
	}
}
