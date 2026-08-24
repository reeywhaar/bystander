package edition

import (
	"context"
	"fmt"
	"testing"
	"time"

	"bystander/internal/ids"
	"bystander/internal/store"
)

// twoFeeds is an instance with a second feed, tagged, so a filter has something to choose
// between.
func twoFeeds(t *testing.T) (*instance, *store.Feed, *store.Tag) {
	t.Helper()
	in := newInstance(t, 6)
	ctx := context.Background()

	tag, err := in.store.CreateTag(ctx, in.principal.ID, "Finance", "", store.DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}
	feed, err := in.store.UpsertFeed(ctx, "https://money.example.com/feed.xml", "Money", "https://money.example.com")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if _, err := in.store.Subscribe(ctx, in.principal.ID, feed.ID, store.DefaultPriority, []string{tag.ID}); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	base := time.Now().Add(-7 * time.Hour)
	items := make([]*store.Item, 6)
	for i := range items {
		items[i] = &store.Item{
			ID:          ids.New(ids.Article),
			FeedID:      feed.ID,
			GUID:        fmt.Sprintf("money-%d", i),
			Title:       fmt.Sprintf("Money %d", i),
			Link:        fmt.Sprintf("https://money.example.com/%d", i),
			Summary:     "<p>A standfirst</p>",
			PublishedAt: base.Add(time.Duration(i) * time.Minute),
			FetchedAt:   base,
		}
	}
	if _, err := in.store.SaveItems(ctx, items); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	return in, feed, tag
}

// page makes a second page and returns it, composed and ready to inspect.
func (in *instance) page(t *testing.T, slug string, patch store.PagePatch) *store.Page {
	t.Helper()
	ctx := context.Background()

	page, err := in.store.CreatePage(ctx, in.principal.ID, slug, slug)
	if err != nil {
		t.Fatalf("CreatePage(): %v", err)
	}
	if err := in.store.UpdatePage(ctx, page.ID, patch); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}
	page, err = in.store.PageByID(ctx, page.ID)
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}
	return page
}

func titlesOf(t *testing.T, st *store.Store, pageID string) []string {
	t.Helper()
	_, items, err := st.CurrentEdition(context.Background(), pageID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	out := make([]string, 0, len(items))
	for _, entry := range items {
		out = append(out, entry.Item.Title)
	}
	return out
}

func TestAPageIncludingATagDrawsOnlyFromIt(t *testing.T) {
	in, _, tag := twoFeeds(t)
	including := store.TagsIncluding
	page := in.page(t, "finances", store.PagePatch{TagFilter: &including, TagIDs: []string{tag.ID}})

	if _, err := in.gen.Generate(context.Background(), page.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	titles := titlesOf(t, in.store, page.ID)
	if len(titles) == 0 {
		t.Fatal("the filtered page is empty")
	}
	for _, title := range titles {
		if title[:5] != "Money" {
			t.Errorf("%q reached a page filtered to the finance tag", title)
		}
	}
}

func TestAPageExcludingATagLeavesItOut(t *testing.T) {
	in, _, tag := twoFeeds(t)
	excluding := store.TagsExcluding
	page := in.page(t, "everything-else", store.PagePatch{TagFilter: &excluding, TagIDs: []string{tag.ID}})

	if _, err := in.gen.Generate(context.Background(), page.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	titles := titlesOf(t, in.store, page.ID)
	if len(titles) == 0 {
		t.Fatal("the filtered page is empty")
	}
	for _, title := range titles {
		if title[:5] == "Money" {
			t.Errorf("%q reached a page excluding the finance tag", title)
		}
	}
}

func TestAPageCanBeHeldToOneFeed(t *testing.T) {
	in, money, _ := twoFeeds(t)
	only := store.FeedsIncluding
	page := in.page(t, "money-only", store.PagePatch{FeedFilter: &only, FeedIDs: []string{money.ID}})

	if _, err := in.gen.Generate(context.Background(), page.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	for _, title := range titlesOf(t, in.store, page.ID) {
		if title[:5] != "Money" {
			t.Errorf("%q reached a page held to one feed", title)
		}
	}
}

// A filter that matches nothing composes nothing rather than erroring, which is the same answer
// a new account gets and for the same reason.
func TestAPageThatMatchesNothingIsSimplyEmpty(t *testing.T) {
	in, _, _ := twoFeeds(t)
	unused, err := in.store.CreateTag(context.Background(), in.principal.ID, "Nobody", "", store.DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}

	including := store.TagsIncluding
	page := in.page(t, "empty", store.PagePatch{TagFilter: &including, TagIDs: []string{unused.ID}})

	ed, err := in.gen.Generate(context.Background(), page.ID)
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	if ed != nil {
		t.Error("a page whose filter matches nothing was given an edition")
	}
}

// Reading is a fact about a person and an article. The same article can sit on a page of
// everything and on a page filtered to one tag at once, and seeing it unread on the next tab
// having just read it reads as a bug rather than as a distinction anybody wanted.
//
// Both editions are written directly rather than composed, so that the overlap is a fact of the
// test rather than something the sampler has to be coaxed into producing. What is being checked
// is SetRead's reach, not how an article comes to be on two pages.
func TestReadingAnArticleReadsItOnEveryPageItIsOn(t *testing.T) {
	in, _, _ := twoFeeds(t)
	ctx := context.Background()

	// SaveItems names an article from its feed and guid, so this is the first of the ones
	// newInstance wrote.
	shared, err := in.store.ItemByID(ctx, ids.Derive(ids.Article, in.feed.ID, "guid-0"))
	if err != nil {
		t.Fatalf("ItemByID(): %v", err)
	}

	second, err := in.store.CreatePage(ctx, in.principal.ID, "Art", "art")
	if err != nil {
		t.Fatalf("CreatePage(): %v", err)
	}
	main, err := in.store.PageByID(ctx, in.pageID())
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}

	pick := []store.Pick{{Item: shared, Rank: 0, Slot: store.SlotLead}}
	for _, page := range []*store.Page{main, second} {
		if _, err := in.store.AddEdition(ctx, page, 1, pick); err != nil {
			t.Fatalf("AddEdition(): %v", err)
		}
	}

	if err := in.store.SetRead(ctx, in.principal.ID, shared.ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}

	for _, page := range []*store.Page{main, second} {
		_, entries, err := in.store.CurrentEdition(ctx, page.ID)
		if err != nil {
			t.Fatalf("CurrentEdition(): %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("%s holds %d articles, want 1", page.ID, len(entries))
		}
		if !entries[0].Read() {
			t.Errorf("%q is still unread on %s after being read elsewhere", shared.Title, page.ID)
		}
	}

	// And unreading reaches just as far, or the two pages disagree the other way round.
	if err := in.store.SetRead(ctx, in.principal.ID, shared.ID, false); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}
	for _, page := range []*store.Page{main, second} {
		_, entries, _ := in.store.CurrentEdition(ctx, page.ID)
		if entries[0].Read() {
			t.Errorf("%q is still read on %s after being unread elsewhere", shared.Title, page.ID)
		}
	}
}

// The point of a page being a view rather than a share.
//
// An art story belongs on a page of everything and on a page of art, and both should show it.
// This was the other way round to begin with: one record of what had been shown, per person, so
// whichever page composed first took the article and the rest could never see it. Measured on
// real feeds, a main page composed after an art page held zero art articles — not a few.
func TestOneArticleReachesEveryPageItBelongsOn(t *testing.T) {
	in, _, tag := twoFeeds(t)
	ctx := context.Background()

	including := store.TagsIncluding
	finances := in.page(t, "finances", store.PagePatch{
		TagFilter: &including, TagIDs: []string{tag.ID},
	})

	// The filtered page first, so if anything were being taken rather than shared, the main
	// page would be the one to come up short.
	if _, err := in.gen.Generate(ctx, finances.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	if _, err := in.gen.Generate(ctx, in.pageID()); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	onFiltered := titlesOf(t, in.store, finances.ID)
	onMain := titlesOf(t, in.store, in.pageID())
	if len(onFiltered) == 0 || len(onMain) == 0 {
		t.Fatalf("pages hold %d and %d articles; both should hold some", len(onFiltered), len(onMain))
	}

	// The main page filters nothing, so everything the filtered page found should be on it too.
	main := map[string]bool{}
	for _, title := range onMain {
		main[title] = true
	}
	for _, title := range onFiltered {
		if !main[title] {
			t.Errorf("%q is on the filtered page but not on the page of everything", title)
		}
	}
}

// Each page keeps its own memory, so composing one twice still spends what it showed.
func TestAPageDoesNotShowItsOwnArticlesTwice(t *testing.T) {
	in, _, _ := twoFeeds(t)
	ctx := context.Background()

	if _, err := in.gen.Generate(ctx, in.pageID()); err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	first := titlesOf(t, in.store, in.pageID())

	// A scheduled turn, which spends what it showed — unlike Regenerate, which hands the
	// unread back first.
	if _, err := in.gen.Generate(ctx, in.pageID()); err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	second := titlesOf(t, in.store, in.pageID())

	seen := map[string]bool{}
	for _, title := range first {
		seen[title] = true
	}
	// Everything these feeds have published fits on one page here, so the first turn spent all
	// of it and the second has nothing fresh left. What comes back is backfill — repeats, which
	// is correct and is the only thing left to show.
	for _, title := range second {
		if !seen[title] {
			t.Errorf("%q is new on the second turn, but everything had already been shown", title)
		}
	}
	if len(second) == 0 {
		t.Error("the second turn produced nothing at all")
	}
}
