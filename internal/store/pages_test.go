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
	if page.TagFilter != TagsIgnored || page.FeedFilter != FeedsAll {
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

	mode := TagsIncluding
	hourly := time.Hour
	size := 20
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		TagFilter:       &mode,
		TagIDs:          []string{tag.ID},
		EditionInterval: &hourly,
		EditionSize:     &size,
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}

	got, err := s.PageByID(t.Context(), page.ID)
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}
	if got.TagFilter != TagsIncluding || len(got.TagIDs) != 1 || got.TagIDs[0] != tag.ID {
		t.Errorf("page = %+v, want it including one tag", got)
	}
	if got.EditionInterval != time.Hour || got.EditionSize != 20 {
		t.Errorf("page = %+v, want hourly and twenty", got)
	}
}

// A list nobody reads should not be waiting to reappear. What somebody last chose while the
// filter was off is not what they mean when they turn it back on.
func TestTurningAFilterOffEmptiesItsList(t *testing.T) {
	s, p := personWithPages(t)

	tag, _ := s.CreateTag(t.Context(), p.ID, "Finance", "", DefaultPriority)
	page, _ := s.CreatePage(t.Context(), p.ID, "Finances", "finances")

	including := TagsIncluding
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		TagFilter: &including, TagIDs: []string{tag.ID},
	}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}

	off := TagsIgnored
	if err := s.UpdatePage(t.Context(), page.ID, PagePatch{TagFilter: &off}); err != nil {
		t.Fatalf("UpdatePage(): %v", err)
	}

	got, _ := s.PageByID(t.Context(), page.ID)
	if len(got.TagIDs) != 0 {
		t.Errorf("tags = %v, want the list cleared with the filter", got.TagIDs)
	}
}

// A filter naming something that is not there would be a page quietly drawing from the wrong
// set, and saying nothing about it.
func TestAPageCannotFilterBySomethingThatIsNotThere(t *testing.T) {
	s, p := personWithPages(t)
	page, _ := s.CreatePage(t.Context(), p.ID, "Finances", "finances")

	including := TagsIncluding
	err := s.UpdatePage(t.Context(), page.ID, PagePatch{
		TagFilter: &including, TagIDs: []string{"t_NOT_A_TAG"},
	})
	if err == nil {
		t.Fatal("UpdatePage() accepted a tag that does not exist")
	}

	// And the refusal took the rest of the save with it, because a page saved half way is a
	// page drawing from the wrong things.
	got, _ := s.PageByID(t.Context(), page.ID)
	if got.TagFilter != TagsIgnored {
		t.Errorf("filter = %q, want the whole save rolled back", got.TagFilter)
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
