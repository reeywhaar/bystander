package store

import (
	"testing"
	"time"
)

func personWithPages(t *testing.T) (*Store, *Principal) {
	t.Helper()
	s := testStore(t)
	p, err := s.CreatePrincipal(t.Context(), "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	return s, p
}

// The whole point of making the main page a row: a person's pages are a list, and there is no
// state in which that list is empty and something has to invent a page.
func TestEverybodyStartsWithOnePage(t *testing.T) {
	s, p := personWithPages(t)

	pages, err := s.Pages(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Pages(): %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("%d pages, want 1", len(pages))
	}
	if !pages[0].IsMain {
		t.Error("the one page a new account has is not marked as the main one")
	}
	if pages[0].Slug != "" {
		t.Errorf("slug = %q, want empty: the main page is served at /", pages[0].Slug)
	}
	if pages[0].ID != MainPageID(p.ID) {
		t.Errorf("id = %q, want %q — derived.db names this page by the same rule",
			pages[0].ID, MainPageID(p.ID))
	}
}

func TestTheMainPageCannotBeRenamedOrRemoved(t *testing.T) {
	s, p := personWithPages(t)
	main := MainPageID(p.ID)

	name := "Something else"
	if err := s.UpdatePage(t.Context(), main, PagePatch{Name: &name}); err == nil {
		t.Error("the main page was renamed")
	}
	slug := "elsewhere"
	if err := s.UpdatePage(t.Context(), main, PagePatch{Slug: &slug}); err == nil {
		t.Error("the main page's address was changed")
	}
	if err := s.DeletePage(t.Context(), main); err == nil {
		t.Error("the main page was removed")
	}

	// And the things that are not fixed about it still are not.
	size := 25
	if err := s.UpdatePage(t.Context(), main, PagePatch{EditionSize: &size}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}
}

func TestAPageIsMadeAndTakenAway(t *testing.T) {
	s, p := personWithPages(t)

	page, err := s.CreatePage(t.Context(), p.ID, "Finances", "finances")
	if err != nil {
		t.Fatalf("CreatePage(): %v", err)
	}
	if page.IsMain {
		t.Error("a new page claims to be the main one")
	}
	// Nothing is filtered until somebody says so, so a new page is a page of everything.
	if len(page.IncludeTagIDs) != 0 || len(page.ExcludeTagIDs) != 0 ||
		len(page.IncludeFeedIDs) != 0 || len(page.ExcludeFeedIDs) != 0 {
		t.Errorf("page = %+v, want no filtering to begin with", page)
	}

	for _, ref := range []string{"finances", page.ID} {
		found, err := s.PageOf(t.Context(), p.ID, ref)
		if err != nil {
			t.Fatalf("PageOf(%q): %v", ref, err)
		}
		if found.ID != page.ID {
			t.Errorf("PageOf(%q) found %s, want %s", ref, found.ID, page.ID)
		}
	}

	// The empty reference is the main page, because that is the main page's address.
	if main, err := s.PageOf(t.Context(), p.ID, ""); err != nil || !main.IsMain {
		t.Errorf("PageOf(\"\") = %v, %v — want the main page", main, err)
	}

	// Main first, then by age — the order of the tab strip.
	pages, _ := s.Pages(t.Context(), p.ID)
	if len(pages) != 2 || !pages[0].IsMain || pages[1].ID != page.ID {
		t.Errorf("pages = %+v, want the main page first", pages)
	}

	if err := s.DeletePage(t.Context(), page.ID); err != nil {
		t.Fatalf("DeletePage(): %v", err)
	}
	if pages, _ := s.Pages(t.Context(), p.ID); len(pages) != 1 {
		t.Errorf("%d pages after removing one, want 1", len(pages))
	}
}

func TestTwoPagesCannotShareAnAddress(t *testing.T) {
	s, p := personWithPages(t)

	if _, err := s.CreatePage(t.Context(), p.ID, "Art", "art"); err != nil {
		t.Fatalf("CreatePage(): %v", err)
	}
	if _, err := s.CreatePage(t.Context(), p.ID, "Also art", "art"); err == nil {
		t.Error("two pages were given the same address")
	}

	// Somebody else's page may have it, because an address is only unique to a person.
	other, err := s.CreatePrincipal(t.Context(), "bob", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if _, err := s.CreatePage(t.Context(), other.ID, "Art", "art"); err != nil {
		t.Errorf("CreatePage() for a second person: %v", err)
	}
}

func TestAnAddressHasToLookLikeOne(t *testing.T) {
	s, p := personWithPages(t)

	for _, slug := range []string{"", "Art", "art page", "art/", "-art", "art-", "ärt"} {
		if _, err := s.CreatePage(t.Context(), p.ID, "Art", slug); err == nil {
			t.Errorf("CreatePage() accepted %q as an address", slug)
		}
	}
	for _, slug := range []string{"art", "art-and-design", "2026"} {
		if _, err := s.CreatePage(t.Context(), p.ID, "Art", slug); err != nil {
			t.Errorf("CreatePage() refused %q: %v", slug, err)
		}
	}
}

func TestAPageIsSavedAllAtOnce(t *testing.T) {
	s, p := personWithPages(t)

	tag, err := s.CreateTag(t.Context(), p.ID, "Finance", "", DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}
	page, err := s.CreatePage(t.Context(), p.ID, "Finances", "finances")
	if err != nil {
		t.Fatalf("CreatePage(): %v", err)
	}

	hourly := time.Hour
	size := 20
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		IncludeTagIDs:   []string{tag.ID},
		EditionInterval: &hourly,
		EditionSize:     &size,
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}

	got, err := s.PageByID(t.Context(), page.ID)
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}
	if len(got.IncludeTagIDs) != 1 || got.IncludeTagIDs[0] != tag.ID {
		t.Errorf("page = %+v, want it including one tag", got)
	}
	if got.EditionInterval != time.Hour || got.EditionSize != 20 {
		t.Errorf("page = %+v, want hourly and twenty", got)
	}
}

// A list nobody reads should not be waiting to reappear. What somebody last chose while the
// filter was off is not what they mean when they turn it back on.
// Emptying a side is how a filter is turned off now, and it must actually empty it.
func TestEmptyingASideClearsIt(t *testing.T) {
	s, p := personWithPages(t)

	feed, _ := s.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if _, err := s.Subscribe(t.Context(), p.ID, feed.ID, DefaultPriority, DefaultArticleWindow, nil); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	page, _ := s.CreatePage(t.Context(), p.ID, "Finances", "finances")

	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		ExcludeFeedIDs: []string{feed.ID},
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		ExcludeFeedIDs: []string{},
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}

	got, _ := s.PageByID(t.Context(), page.ID)
	if len(got.ExcludeFeedIDs) != 0 {
		t.Errorf("feeds = %v, want the side cleared when it was named", got.ExcludeFeedIDs)
	}
}

// The two sides of a list are separate, and saving one must not take the other with it.
//
// They share a table, so the delete-then-insert that replaces a side has to be told which side
// it is replacing. Without that, saving the drop side would empty the draw-from side — and the
// page would silently widen to everything, which is the opposite of what the person pressing
// save was doing.
func TestSavingOneSideLeavesTheOtherAlone(t *testing.T) {
	s, p := personWithPages(t)

	finance, _ := s.CreateTag(t.Context(), p.ID, "Finance", "", DefaultPriority)
	crypto, _ := s.CreateTag(t.Context(), p.ID, "Crypto", "", DefaultPriority)
	page, _ := s.CreatePage(t.Context(), p.ID, "Finances", "finances")

	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		IncludeTagIDs: []string{finance.ID},
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		ExcludeTagIDs: []string{crypto.ID},
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}

	got, _ := s.PageByID(t.Context(), page.ID)
	if len(got.IncludeTagIDs) != 1 || got.IncludeTagIDs[0] != finance.ID {
		t.Errorf("draws from %v, want just Finance", got.IncludeTagIDs)
	}
	if len(got.ExcludeTagIDs) != 1 || got.ExcludeTagIDs[0] != crypto.ID {
		t.Errorf("drops %v, want just Crypto", got.ExcludeTagIDs)
	}
}

// Taking a tag and dropping it is a contradiction, not a filter with an unlucky answer.
//
// Refused when the page is saved rather than settled by whichever side is applied last, and
// refused across requests too: a save that only sets the drop side still has to agree with the
// draw-from side already there. The interface cannot produce it — one switch cannot hold two
// answers — which is a reason to check it here rather than a reason not to.
func TestAPageCannotDrawFromATagAndDropIt(t *testing.T) {
	s, p := personWithPages(t)

	tag, _ := s.CreateTag(t.Context(), p.ID, "Finance", "", DefaultPriority)
	page, _ := s.CreatePage(t.Context(), p.ID, "Finances", "finances")

	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		IncludeTagIDs: []string{tag.ID}, ExcludeTagIDs: []string{tag.ID},
	}); err == nil {
		t.Error("UpdatePage() accepted a tag on both lists in one save")
	}

	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		IncludeTagIDs: []string{tag.ID},
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		ExcludeTagIDs: []string{tag.ID},
	}); err == nil {
		t.Error("UpdatePage() accepted a tag it already draws from onto the drop list")
	}
}

// A filter naming something that is not there would be a page quietly drawing from the wrong
// set, and saying nothing about it.
func TestAPageCannotFilterBySomethingThatIsNotThere(t *testing.T) {
	s, p := personWithPages(t)
	page, _ := s.CreatePage(t.Context(), p.ID, "Finances", "finances")

	err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		IncludeTagIDs: []string{"t_NOT_A_TAG"},
	})
	if err == nil {
		t.Fatal("UpdatePage() accepted a tag that does not exist")
	}

	// And the refusal took the rest of the save with it, because a page saved half way is a
	// page drawing from the wrong things.
	got, _ := s.PageByID(t.Context(), page.ID)
	if len(got.IncludeTagIDs) != 0 {
		t.Errorf("tags = %v, want the whole save rolled back", got.IncludeTagIDs)
	}
}

func TestOnlyDuePagesAreDue(t *testing.T) {
	s, p := personWithPages(t)

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })

	// The main page was stamped from the real clock when the account was made, and this test
	// then moves the clock to a fixed date. Without restamping it, whether the main page is
	// due depends on whether the real time of day is before or after noon UTC — which is a
	// test that passes all morning and fails all afternoon.
	if err := s.ScheduleNextEdition(t.Context(), MainPageID(p.ID), now); err != nil {
		t.Fatalf("ScheduleNextEdition(): %v", err)
	}

	later, _ := s.CreatePage(t.Context(), p.ID, "Later", "later")
	if err := s.ScheduleNextEdition(t.Context(), later.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("ScheduleNextEdition(): %v", err)
	}

	due, err := s.DuePages(t.Context())
	if err != nil {
		t.Fatalf("DuePages(): %v", err)
	}
	if len(due) != 1 || due[0].ID != MainPageID(p.ID) {
		t.Errorf("due = %+v, want only the main page", due)
	}

	// And it comes round when its turn arrives.
	now = now.Add(2 * time.Hour)
	if due, _ := s.DuePages(t.Context()); len(due) != 2 {
		t.Errorf("%d pages due once the hour has passed, want 2", len(due))
	}
}
