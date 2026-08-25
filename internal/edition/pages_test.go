package edition

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"bystander/internal/ids"
	"bystander/internal/store"
)

// twoFeeds is an instance with a second feed, tagged, so a filter has something to choose
// between. Each feed gets `each` articles.
func twoFeeds(t *testing.T, each int) (*instance, *store.Feed, *store.Tag) {
	t.Helper()
	in := newInstance(t, each)

	tag, err := in.store.CreateTag(context.Background(), in.principal.ID, "Finance", "", store.DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}
	return in, in.addFeed(t, "Money", "money", each, tag.ID), tag
}

// addFeed adds one followed feed with `each` articles, carrying the given tags.
//
// Titles are prefixed with the name, which is how every assertion here tells one feed's
// articles from another's.
func (in *instance) addFeed(t *testing.T, name, host string, each int, tagIDs ...string) *store.Feed {
	t.Helper()
	ctx := context.Background()

	url := "https://" + host + ".example.com"
	feed, err := in.store.UpsertFeed(ctx, url+"/feed.xml", name, url)
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if _, err := in.store.Subscribe(ctx, in.principal.ID, feed.ID,
		store.DefaultPriority, store.DefaultArticleWindow, tagIDs); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	base := time.Now().Add(-time.Duration(each+1) * time.Hour)
	items := make([]*store.Item, each)
	for i := range items {
		items[i] = &store.Item{
			ID:          ids.New(ids.Article),
			FeedID:      feed.ID,
			GUID:        fmt.Sprintf("%s-%d", host, i),
			Title:       fmt.Sprintf("%s %d", name, i),
			Link:        fmt.Sprintf("%s/%d", url, i),
			Summary:     "<p>A standfirst</p>",
			PublishedAt: base.Add(time.Duration(i) * time.Minute),
			FetchedAt:   base,
		}
	}
	if _, err := in.store.SaveItems(ctx, items); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	return feed
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
	_, items, err := st.CurrentEdition(context.Background(), pageID, "")
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
	in, _, tag := twoFeeds(t, 6)
	page := in.page(t, "finances", store.PagePatch{IncludeTagIDs: []string{tag.ID}})

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
	in, _, tag := twoFeeds(t, 6)
	page := in.page(t, "everything-else", store.PagePatch{ExcludeTagIDs: []string{tag.ID}})

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

// A feed switched on is on the page even when the tags said otherwise.
//
// This is the half a narrowing filter could never express. The old feed filter could only take
// the tags' answer and cut it down further, so "everything but Crypto, except keep this one" —
// which is the ordinary thing anybody wants to say about one publisher — had no shape.
func TestAFeedSwitchedOnBeatsAnExcludedTag(t *testing.T) {
	in, money, tag := twoFeeds(t, 6)
	page := in.page(t, "kept", store.PagePatch{
		ExcludeTagIDs:  []string{tag.ID},
		IncludeFeedIDs: []string{money.ID},
	})

	if _, err := in.gen.Generate(context.Background(), page.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	var kept bool
	for _, title := range titlesOf(t, in.store, page.ID) {
		if title[:5] == "Money" {
			kept = true
		}
	}
	if !kept {
		t.Error("a feed switched on was dropped by a tag it carries; the switch is meant to overrule the tags")
	}
}

// And the other way: a feed switched off stays off however the tags voted.
//
// A second feed carries the same tag, so the page is not empty — otherwise this would pass on a
// page that composed nothing at all, which proves the feed was dropped only by proving that
// everything was.
func TestAFeedSwitchedOffBeatsAnIncludedTag(t *testing.T) {
	in, money, tag := twoFeeds(t, 6)
	in.addFeed(t, "Bonds", "bonds", 6, tag.ID)

	page := in.page(t, "dropped", store.PagePatch{
		IncludeTagIDs:  []string{tag.ID},
		ExcludeFeedIDs: []string{money.ID},
	})
	if _, err := in.gen.Generate(context.Background(), page.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	var kept int
	for _, title := range titlesOf(t, in.store, page.ID) {
		if strings.HasPrefix(title, "Money") {
			t.Errorf("%q reached a page that switched its feed off", title)
		}
		if strings.HasPrefix(title, "Bonds") {
			kept++
		}
	}
	if kept == 0 {
		t.Error("the other feed carrying that tag was dropped too; the switch is about one feed")
	}
}

// The case the two tag sides exist for, which is the one a single mode could not say: draw from
// a tag, then drop the feeds that also carry another. Both tags are on the same feed, so
// nothing but the ordering between the two sides decides the answer.
func TestATagCanBeDrawnFromAndAnOverlappingOneDropped(t *testing.T) {
	in, _, finance := twoFeeds(t, 6)
	ctx := context.Background()

	crypto, err := in.store.CreateTag(ctx, in.principal.ID, "Crypto", "", store.DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}
	// One feed carrying both tags, and one carrying only the first. The overlap is the whole
	// point: under a single mode, excluding Crypto meant giving up the include, and including
	// Finance meant taking Coins along with it.
	in.addFeed(t, "Coins", "coins", 6, finance.ID, crypto.ID)

	page := in.page(t, "finance-not-crypto", store.PagePatch{
		IncludeTagIDs: []string{finance.ID},
		ExcludeTagIDs: []string{crypto.ID},
	})
	if _, err := in.gen.Generate(ctx, page.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	var kept int
	for _, title := range titlesOf(t, in.store, page.ID) {
		if strings.HasPrefix(title, "Coins") {
			t.Errorf("%q is Finance and Crypto at once; drawing from one and dropping the other must drop it", title)
		}
		if strings.HasPrefix(title, "Money") {
			kept++
		}
	}
	if kept == 0 {
		t.Error("the Finance feed that is not Crypto was dropped too; the exclude is meant to take only the overlap")
	}
}

// A filter that matches nothing composes nothing rather than erroring, which is the same answer
// a new account gets and for the same reason.
func TestAPageThatMatchesNothingIsSimplyEmpty(t *testing.T) {
	in, _, _ := twoFeeds(t, 6)
	unused, err := in.store.CreateTag(context.Background(), in.principal.ID, "Nobody", "", store.DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}

	page := in.page(t, "empty", store.PagePatch{IncludeTagIDs: []string{unused.ID}})

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
	in, _, _ := twoFeeds(t, 6)
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
		// Read as the person who read it: the mark is theirs and the join is against them.
		_, entries, err := in.store.CurrentEdition(ctx, page.ID, in.principal.ID)
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
		_, entries, _ := in.store.CurrentEdition(ctx, page.ID, in.principal.ID)
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
	in, _, tag := twoFeeds(t, 6)
	ctx := context.Background()

	finances := in.page(t, "finances", store.PagePatch{
		IncludeTagIDs: []string{tag.ID},
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
	in, _, _ := twoFeeds(t, 6)
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

// Reading something on one page must not put it, greyed, on the next page another one composes.
//
// "Shown" is per page and "read" is per person. With one front page the second was a subset of
// the first — you could only read what that page had shown you — so this never came up. With
// several it is not: an article read on a page of comics has never been shown on the page of
// everything, so it arrived there as a fresh candidate and landed already greyed, because the
// read mark follows the person. Nineteen of forty-six articles on one real page were like that.
func TestAPageDoesNotDrawWhatYouHaveAlreadyRead(t *testing.T) {
	in, _, tag := twoFeeds(t, 30)
	ctx := context.Background()

	money := in.page(t, "money", store.PagePatch{IncludeTagIDs: []string{tag.ID}})

	if _, err := in.gen.Generate(ctx, money.ID); err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	_, onMoney, err := in.store.CurrentEdition(ctx, money.ID, "")
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	if len(onMoney) == 0 {
		t.Fatal("the filtered page is empty")
	}

	read := map[string]string{}
	for _, entry := range onMoney {
		if err := in.store.SetRead(ctx, in.principal.ID, entry.Item.ID, true); err != nil {
			t.Fatalf("SetRead(): %v", err)
		}
		read[entry.Item.ID] = entry.Item.Title
	}

	// Small enough that the page of everything is filled by things nobody has read — so
	// anything read that turns up was drawn as fresh rather than used as backfill, which is
	// a different thing and stays allowed.
	in.size(t, 10)
	if _, err := in.gen.Generate(ctx, in.pageID()); err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	_, onMain, err := in.store.CurrentEdition(ctx, in.pageID(), in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	if len(onMain) == 0 {
		t.Fatal("the page of everything is empty")
	}
	for _, entry := range onMain {
		if title, ok := read[entry.Item.ID]; ok {
			t.Errorf("%q was read on another page and is on this freshly composed one", title)
		}
		if entry.Read() {
			t.Errorf("%q arrived on a freshly composed page already greyed", entry.Item.Title)
		}
	}
}

// A page read right through still turns, and still fills.
//
// This is the shape the fallback exists for, and the one it used to fail at. A read mark is a
// fact about a person, so an article read anywhere is not a fresh candidate here — which means
// a page somebody keeps up with is a page with nothing fresh at all, and its repeats are the
// only thing there is to compose from. Reaching them needs the fallback to be drawn through
// its own buckets: the fresh pool's list only the feeds that had something fresh, and here no
// feed does.
func TestAPageReadRightThroughStillFills(t *testing.T) {
	in := newInstance(t, 6)
	ctx := context.Background()

	tag, err := in.store.CreateTag(ctx, in.principal.ID, "Comics", "", store.DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}
	for _, name := range []string{"Poorly", "Perry", "Talk"} {
		in.addFeed(t, name, strings.ToLower(name), 8, tag.ID)
	}

	size := 12
	page := in.page(t, "comics", store.PagePatch{IncludeTagIDs: []string{tag.ID}, EditionSize: &size})
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	// Two turns take all twenty-four articles, and everything is read as it goes. After this
	// there is nothing unshown and nothing unread anywhere in the three feeds.
	for turn := range 2 {
		if err := in.gen.GenerateAndSchedule(ctx, page, now.Add(time.Duration(turn)*24*time.Hour)); err != nil {
			t.Fatalf("turn %d: %v", turn+1, err)
		}
		_, items, err := in.store.CurrentEdition(ctx, page.ID, "")
		if err != nil {
			t.Fatalf("turn %d: CurrentEdition(): %v", turn+1, err)
		}
		if len(items) != size {
			t.Fatalf("turn %d holds %d articles, want %d", turn+1, len(items), size)
		}
		for _, entry := range items {
			if err := in.store.SetRead(ctx, in.principal.ID, entry.Item.ID, true); err != nil {
				t.Fatalf("SetRead(): %v", err)
			}
		}
		if page, err = in.store.PageByID(ctx, page.ID); err != nil {
			t.Fatalf("PageByID(): %v", err)
		}
	}

	spent, _, err := in.store.CurrentEdition(ctx, page.ID, "")
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}

	if err := in.gen.GenerateAndSchedule(ctx, page, now.Add(48*time.Hour)); err != nil {
		t.Fatalf("the third turn: %v", err)
	}

	ed, items, err := in.store.CurrentEdition(ctx, page.ID, "")
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	// The page not turning at all is how this failed, so it is what to check first: a stale
	// edition left in place reads as "a full page" to any assertion about length.
	if ed.ID == spent.ID {
		t.Fatal("the page did not turn — nothing was fresh, and its own repeats were out of reach")
	}
	if len(items) != size {
		t.Fatalf("the page holds %d articles, want a full %d of repeats", len(items), size)
	}

	// And from all three feeds, not just whichever one the fresh pool happened to name.
	feeds := map[string]bool{}
	for _, entry := range items {
		feeds[entry.Item.FeedID] = true
	}
	if len(feeds) != 3 {
		t.Errorf("the page draws from %d feeds, want all three: %v", len(feeds), titlesOf(t, in.store, page.ID))
	}
}
