package edition

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"bystander/internal/ids"
	"bystander/internal/store"
)

// instance is a store with one account, one feed, and some articles in it.
type instance struct {
	store     *store.Store
	gen       *Generator
	principal *store.Principal
	feed      *store.Feed
}

func newInstance(t *testing.T, articles int) *instance {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	t.Cleanup(func() { st.Close() })

	p, err := st.CreatePrincipal(ctx, "alice", "correct-horse", store.RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	feed, err := st.UpsertFeed(ctx, "https://example.com/feed.xml", "The Example", "https://example.com")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if _, err := st.Subscribe(ctx, p.ID, feed.ID, store.DefaultPriority, nil); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	items := make([]*store.Item, articles)
	// Recent, because a page only takes articles inside its window.
	base := time.Now().Add(-time.Duration(articles+1) * time.Hour)
	for i := range articles {
		items[i] = &store.Item{
			ID:          ids.New(ids.Article),
			FeedID:      feed.ID,
			GUID:        fmt.Sprintf("guid-%d", i),
			Title:       fmt.Sprintf("Story %d", i),
			Link:        fmt.Sprintf("https://example.com/%d", i),
			Summary:     "<p>A standfirst</p>",
			ImageURL:    "https://example.com/pic.png",
			PublishedAt: base.Add(time.Duration(i) * time.Hour),
			FetchedAt:   base,
		}
	}
	if _, err := st.SaveItems(ctx, items); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}

	return &instance{
		store:     st,
		gen:       NewGenerator(st, slog.New(slog.NewTextHandler(io.Discard, nil))),
		principal: p,
		feed:      feed,
	}
}

func (in *instance) size(t *testing.T, articles int) {
	t.Helper()
	if err := in.store.UpdateSettings(context.Background(), in.principal.ID,
		store.SettingsPatch{EditionSize: &articles}); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
}

// titles is what is on the live page, by title, so a failure names the articles.
func (in *instance) titles(t *testing.T) []string {
	t.Helper()
	_, items, err := in.store.CurrentEdition(context.Background(), in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	out := make([]string, 0, len(items))
	for _, entry := range items {
		out = append(out, entry.Item.Title)
	}
	return out
}

func (in *instance) scheduledTurn(t *testing.T, at time.Time) {
	t.Helper()
	settings, err := in.store.Settings(context.Background(), in.principal.ID)
	if err != nil {
		t.Fatalf("Settings(): %v", err)
	}
	if err := in.gen.GenerateAndSchedule(context.Background(), settings, at); err != nil {
		t.Fatalf("GenerateAndSchedule(): %v", err)
	}
}

// A scheduled turn is time passing: the page it replaces is gone, articles and all.
func TestScheduledTurnsNeverRepeat(t *testing.T) {
	in := newInstance(t, 20)
	in.size(t, 10)

	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	in.scheduledTurn(t, now)
	first := in.titles(t)
	if len(first) != 10 {
		t.Fatalf("the first page holds %d articles, want 10", len(first))
	}

	in.scheduledTurn(t, now.Add(24*time.Hour))
	second := in.titles(t)
	if len(second) != 10 {
		t.Fatalf("the second page holds %d articles, want 10", len(second))
	}

	seen := map[string]bool{}
	for _, title := range first {
		seen[title] = true
	}
	for _, title := range second {
		if seen[title] {
			t.Errorf("%q appeared on two consecutive scheduled pages", title)
		}
	}
}

// A manual regeneration is not time passing. Nothing has elapsed — somebody has asked for a
// different page — so articles they never looked at must come back rather than being spent.
func TestRegenerateReturnsUnreadArticles(t *testing.T) {
	in := newInstance(t, 12)
	in.size(t, 10)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	in.scheduledTurn(t, now)
	first := in.titles(t)

	if _, err := in.gen.Regenerate(ctx, in.principal.ID, now); err != nil {
		t.Fatalf("Regenerate(): %v", err)
	}
	second := in.titles(t)
	if len(second) != 10 {
		t.Fatalf("the re-rolled page holds %d articles, want 10", len(second))
	}

	// With twelve articles and a page of ten, a spend-the-pool regeneration could only
	// have found two. Overlap is the point here, not a failure.
	overlap := 0
	seen := map[string]bool{}
	for _, title := range first {
		seen[title] = true
	}
	for _, title := range second {
		if seen[title] {
			overlap++
		}
	}
	if overlap < 8 {
		t.Errorf("only %d of the first page's articles survived the re-roll; they were spent", overlap)
	}
}

// Read articles are dealt with either way, so they stay spent.
func TestRegenerateKeepsReadArticlesSpent(t *testing.T) {
	in := newInstance(t, 12)
	in.size(t, 10)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	in.scheduledTurn(t, now)

	_, items, err := in.store.CurrentEdition(ctx, in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	read := items[0].Item
	if err := in.store.SetRead(ctx, in.principal.ID, read.ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}

	if _, err := in.gen.Regenerate(ctx, in.principal.ID, now); err != nil {
		t.Fatalf("Regenerate(): %v", err)
	}
	for _, title := range in.titles(t) {
		if title == read.Title {
			t.Fatalf("%q was read, and came back anyway", title)
		}
	}
}

// The button has to survive being pressed repeatedly while somebody tunes priorities. That
// is the whole reason a re-roll exists.
func TestRegenerateSurvivesRepeatedPresses(t *testing.T) {
	in := newInstance(t, 15)
	in.size(t, 10)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	in.scheduledTurn(t, now)
	for press := range 5 {
		if _, err := in.gen.Regenerate(ctx, in.principal.ID, now); err != nil {
			t.Fatalf("press %d: %v", press+1, err)
		}
		if got := len(in.titles(t)); got != 10 {
			t.Fatalf("press %d gave %d articles, want 10", press+1, got)
		}
	}
}

// Everything read and nothing new is a real answer, and it has to be distinguishable from
// a failure.
func TestRegenerateRefusesWhenEverythingIsRead(t *testing.T) {
	in := newInstance(t, 5)
	in.size(t, 10)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	in.scheduledTurn(t, now)

	_, items, err := in.store.CurrentEdition(ctx, in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	for _, entry := range items {
		if err := in.store.SetRead(ctx, in.principal.ID, entry.Item.ID, true); err != nil {
			t.Fatalf("SetRead(): %v", err)
		}
	}

	_, err = in.gen.Regenerate(ctx, in.principal.ID, now)
	if err == nil {
		t.Fatal("Regenerate() composed a page out of nothing")
	}
	if !isConflict(err) {
		t.Errorf("Regenerate() = %v, want a conflict", err)
	}
}

func isConflict(err error) bool {
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if err == store.ErrConflict {
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// A page with room left over and nothing fresh to put in it looks broken rather than
// honest, so the rest is filled from what has been seen before.
func TestAShortPageIsFilledFromWhatWasSeen(t *testing.T) {
	in := newInstance(t, 12)
	in.size(t, 10)
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	in.scheduledTurn(t, now)
	if got := len(in.titles(t)); got != 10 {
		t.Fatalf("the first page holds %d, want 10", got)
	}

	// Only two articles left unshown, and a page that wants ten.
	in.scheduledTurn(t, now.Add(24*time.Hour))

	second := in.titles(t)
	if len(second) != 10 {
		t.Fatalf("the second page holds %d articles, want a full 10 — eight of them repeats",
			len(second))
	}

	// The two that had never been shown are certainly on it.
	seen := map[string]bool{}
	for _, title := range second {
		seen[title] = true
	}
	if !seen["Story 10"] || !seen["Story 11"] {
		t.Errorf("the unshown articles are missing from %v", second)
	}
}

// A repeat that was actually read comes back greyed. One that merely went past unread comes
// back plain, which is fair — it was never read.
func TestARepeatKeepsItsReadMark(t *testing.T) {
	in := newInstance(t, 10)
	in.size(t, 10)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	in.scheduledTurn(t, now)
	_, items, err := in.store.CurrentEdition(ctx, in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	read := items[0].Item
	if err := in.store.SetRead(ctx, in.principal.ID, read.ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}

	// Nothing fresh at all, so the whole next page is repeats.
	in.scheduledTurn(t, now.Add(24*time.Hour))

	_, next, err := in.store.CurrentEdition(ctx, in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	if len(next) == 0 {
		t.Fatal("the page came back empty rather than repeating")
	}

	marked, plain := 0, 0
	for _, entry := range next {
		if entry.Item.ID == read.ID {
			if !entry.Read() {
				t.Errorf("%q was read and came back looking new", entry.Item.Title)
			}
			marked++
			continue
		}
		if entry.Read() {
			t.Errorf("%q was never read and came back marked", entry.Item.Title)
		}
		plain++
	}
	if marked == 0 {
		t.Error("the article that was read did not come back at all")
	}
	if plain == 0 {
		t.Error("nothing unread came back")
	}
}

// Preferring what went past unread: those are closer to new than what somebody has already
// finished with.
func TestBackfillPrefersWhatWasNeverRead(t *testing.T) {
	in := newInstance(t, 20)
	in.size(t, 10)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	// Show everything across two pages, reading the first page through.
	in.scheduledTurn(t, now)
	_, first, _ := in.store.CurrentEdition(ctx, in.principal.ID)
	for _, entry := range first {
		if err := in.store.SetRead(ctx, in.principal.ID, entry.Item.ID, true); err != nil {
			t.Fatalf("SetRead(): %v", err)
		}
	}
	in.scheduledTurn(t, now.Add(24*time.Hour))

	// Now everything has been shown; ten were read and ten were not. A page of ten should
	// be the ten nobody read.
	in.scheduledTurn(t, now.Add(48*time.Hour))

	_, third, err := in.store.CurrentEdition(ctx, in.principal.ID)
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	if len(third) != 10 {
		t.Fatalf("the page holds %d, want 10", len(third))
	}

	wasRead := 0
	for _, entry := range third {
		if entry.Read() {
			wasRead++
		}
	}
	if wasRead > 3 {
		t.Errorf("%d of 10 had already been read; unread repeats should come first", wasRead)
	}
}
