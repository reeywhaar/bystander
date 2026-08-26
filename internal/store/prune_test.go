package store

import (
	"context"
	"testing"
	"time"
)

// seedFeed makes a feed, subscribes somebody to it with a window, and fills it with articles
// fetched a given number of days ago.
func seedFeed(t *testing.T, s *Store, principalID, name string, window time.Duration, ages ...int) string {
	t.Helper()
	ctx := context.Background()

	feed, err := s.UpsertFeed(ctx, "https://example.com/"+name+".xml", name, "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if _, err := s.Subscribe(ctx, principalID, feed.ID, DefaultPriority, window, nil); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	items := make([]*Item, 0, len(ages))
	for i, days := range ages {
		when := s.Now().AddDate(0, 0, -days)
		items = append(items, &Item{
			FeedID:      feed.ID,
			GUID:        name + "-" + time.Duration(i).String(),
			Title:       name,
			Link:        "https://example.com/" + name,
			PublishedAt: when,
			FetchedAt:   when,
		})
	}
	if _, err := s.SaveItems(ctx, items); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	return feed.ID
}

func countItems(t *testing.T, s *Store, feedID string) int {
	t.Helper()
	var n int
	if err := s.derived.QueryRowContext(context.Background(),
		`SELECT count(*) FROM items WHERE feed_id = ?`, feedID).Scan(&n); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return n
}

// One number for the whole instance was one number too few: it took the longest window
// chosen anywhere, so a webcomic somebody wanted a year of made a news feed at ninety
// articles a day keep a year as well — thirty thousand articles nobody had asked for, to
// serve a page that shows sixty.
func TestPruningIsPerFeed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	// Both hold articles from today, forty days ago, and two hundred days ago.
	news := seedFeed(t, s, p.ID, "news", 7*24*time.Hour, 0, 40, 200)
	archive := seedFeed(t, s, p.ID, "archive", 365*24*time.Hour, 0, 40, 200)

	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatalf("PruneItems(): %v", err)
	}

	// A week is under the thirty-day floor, so the news feed keeps thirty days: today's
	// survives and the other two do not.
	if got := countItems(t, s, news); got != 1 {
		t.Errorf("the news feed kept %d articles, want 1 — only today's is inside thirty days", got)
	}
	// And the feed somebody asked a year of is untouched by the other's shorter window.
	if got := countItems(t, s, archive); got != 3 {
		t.Errorf("the archive kept %d articles, want all 3", got)
	}
}

// "No limit" means no limit. It took a one-year ceiling, and that was wrong for a reason
// worth a test: how far back a page reaches bounds when an article was *published*, and
// pruning goes by when it was *fetched*. A feed whose every article was written years ago is
// an ordinary thing to want all of.
func TestAnUnlimitedFeedLosesNothingToAge(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	// Fetched two and three years ago, which any ceiling would have taken.
	comic := seedFeed(t, s, p.ID, "comic", 0, 0, 730, 1095)

	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, comic); got != 3 {
		t.Errorf("an unlimited feed kept %d articles, want all 3", got)
	}
}

// The shelf has a finite length even where the calendar does not.
func TestTheCeilingHoldsAFeedToItsNewest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	// Ten articles, one a day, on a feed nothing would prune by age.
	ages := make([]int, 10)
	for i := range ages {
		ages[i] = i
	}
	feed := seedFeed(t, s, p.ID, "flood", 0, ages...)

	cut, err := s.CapItemsPerFeed(ctx, []string{feed}, 4)
	if err != nil {
		t.Fatalf("CapItemsPerFeed(): %v", err)
	}
	if cut[feed] != 6 {
		t.Errorf("the ceiling took %d, want 6", cut[feed])
	}
	if got := countItems(t, s, feed); got != 4 {
		t.Fatalf("the feed holds %d articles, want 4", got)
	}

	// The newest four, by publication — the order a page draws in.
	rows, err := s.derived.QueryContext(ctx,
		`SELECT published_at FROM items WHERE feed_id = ? ORDER BY published_at DESC`, feed)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	oldest := s.Now()
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			t.Fatal(err)
		}
		if when := time.Unix(at, 0); when.Before(oldest) {
			oldest = when
		}
	}
	if s.Now().Sub(oldest) > 4*24*time.Hour {
		t.Errorf("the oldest kept article is %s old, so the ceiling took the wrong end",
			s.Now().Sub(oldest))
	}

	// A ceiling nothing reaches takes nothing, and says so by returning no feeds at all.
	cut, err = s.CapItemsPerFeed(ctx, []string{feed}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cut) != 0 {
		t.Errorf("a ceiling above the feed reported %v", cut)
	}
}

// An article vanishing out from under a composed page is a hole in something somebody is
// reading, so nothing on a live edition is taken — by age or by the ceiling.
func TestPruningSparesWhatAPageIsShowing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	feed := seedFeed(t, s, p.ID, "news", 7*24*time.Hour, 200, 201)

	var ids []string
	rows, err := s.derived.QueryContext(ctx, `SELECT id FROM items WHERE feed_id = ?`, feed)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	pages, err := s.Pages(ctx, p.ID)
	if err != nil || len(pages) == 0 {
		t.Fatalf("Pages(): %v", err)
	}
	if _, err := s.AddEdition(ctx, pages[0], 1, []Pick{{Item: &Item{ID: ids[0]}, Slot: SlotLead}}); err != nil {
		t.Fatalf("AddEdition(): %v", err)
	}

	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, feed); got != 1 {
		t.Fatalf("the feed holds %d articles, want the one a page is showing", got)
	}

	// The ceiling spares it too.
	if _, err := s.CapItemsPerFeed(ctx, []string{feed}, 0+1); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, feed); got != 1 {
		t.Errorf("the ceiling took an article a page is showing")
	}
}

// A feed nobody follows constrains nothing, and everything it has is due to be collected.
func TestPruningTakesEverythingFromAnUnfollowedFeed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	kept := seedFeed(t, s, p.ID, "kept", 0, 0)
	dropped := seedFeed(t, s, p.ID, "dropped", 0, 0)

	subs, err := s.ListSubscriptions(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range subs {
		if sub.FeedID == dropped {
			if err := s.DeleteSubscription(ctx, p.ID, sub.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, dropped); got != 0 {
		t.Errorf("an unfollowed feed kept %d articles", got)
	}
	if got := countItems(t, s, kept); got != 1 {
		t.Errorf("the followed feed lost articles: %d left", got)
	}
}

// The ceiling is a judgement about reading, not about age: a feed that put out more than a
// thousand articles yesterday has nothing to offer past them, however far back somebody asked
// to reach. Everything here is a day old and inside a year-long window, and the ceiling still
// decides.
func TestTheCeilingIgnoresTheWindow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := s.UpsertFeed(ctx, "https://example.com/wire.xml", "Wire", "")
	if err != nil {
		t.Fatal(err)
	}
	feed := wire.ID
	if _, err := s.Subscribe(ctx, p.ID, feed, DefaultPriority, 365*24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}

	// All published yesterday, a minute apart, so nothing is old and everything is inside
	// the window.
	yesterday := s.Now().AddDate(0, 0, -1)
	items := make([]*Item, 0, MaxItemsPerFeed+50)
	for i := range MaxItemsPerFeed + 50 {
		items = append(items, &Item{
			FeedID:      feed,
			GUID:        "wire-" + time.Duration(i).String(),
			Title:       "Wire",
			Link:        "https://example.com/wire",
			PublishedAt: yesterday.Add(time.Duration(i) * time.Minute),
			FetchedAt:   s.Now(),
		})
	}
	if _, err := s.SaveItems(ctx, items); err != nil {
		t.Fatal(err)
	}

	// Age takes nothing: everything was fetched today, inside a year.
	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, feed); got != MaxItemsPerFeed+50 {
		t.Fatalf("age pruning took %d articles it had no business taking",
			MaxItemsPerFeed+50-got)
	}

	// The ceiling does.
	cut, err := s.CapItemsPerFeed(ctx, []string{feed}, MaxItemsPerFeed)
	if err != nil {
		t.Fatal(err)
	}
	if cut[feed] != 50 {
		t.Errorf("the ceiling took %d, want 50", cut[feed])
	}
	if got := countItems(t, s, feed); got != MaxItemsPerFeed {
		t.Errorf("the feed holds %d articles, want %d", got, MaxItemsPerFeed)
	}

	// And it kept the newest: the very first minute of the batch is the one that went.
	var oldest int64
	if err := s.derived.QueryRowContext(ctx,
		`SELECT min(published_at) FROM items WHERE feed_id = ?`, feed).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	// The batch ran from minute 0 to minute 1049, so keeping the newest thousand leaves
	// minute 50 as the oldest. A second of slack, because a stored timestamp is whole
	// seconds and the clock this was built from is not.
	if want := yesterday.Add(50 * time.Minute); time.Unix(oldest, 0).Before(want.Add(-time.Second)) {
		t.Errorf("the oldest kept is %s, want about %s — the ceiling took from the wrong end",
			time.Unix(oldest, 0), want)
	}
}

// Nothing on anybody's live edition is swept — not by age, not by the ceiling, and not
// because the person whose page it is happens not to be the one whose settings were consulted.
//
// The guard is `NOT IN (SELECT item_id FROM edition_items)`, which is deliberately not scoped
// to a person: an article held by one reader's page is held for everybody, because a page is
// composed once and read from wherever. What makes that safe is the sweep's order —
// PruneOldEditions runs first and its edition_items rows cascade away, so what is left is
// exactly the current editions. This is the test that says so.
func TestPruningSparesEveryUsersLiveEdition(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	ada, err := s.CreatePrincipal(ctx, "ada", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreatePrincipal(ctx, "bob", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	// One feed, followed by both on the shortest window there is, holding three articles
	// fetched long enough ago that every one of them is due to go.
	feed := seedFeed(t, s, ada.ID, "news", 24*time.Hour, 200, 201, 202)
	if _, err := s.Subscribe(ctx, bob.ID, feed, DefaultPriority, 24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}

	var items []string
	rows, err := s.derived.QueryContext(ctx,
		`SELECT id FROM items WHERE feed_id = ? ORDER BY published_at DESC`, feed)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		items = append(items, id)
	}
	rows.Close()
	if len(items) != 3 {
		t.Fatalf("seeded %d articles, want 3", len(items))
	}

	// One on each person's page. The third is on nobody's, and is the control.
	for i, who := range []*Principal{ada, bob} {
		pages, err := s.Pages(ctx, who.ID)
		if err != nil || len(pages) == 0 {
			t.Fatalf("Pages(%s): %v", who.Username, err)
		}
		if _, err := s.AddEdition(ctx, pages[0], int64(i+1),
			[]Pick{{Item: &Item{ID: items[i]}, Slot: SlotLead}}); err != nil {
			t.Fatalf("AddEdition(%s): %v", who.Username, err)
		}
	}

	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, feed); got != 2 {
		t.Fatalf("after pruning by age the feed holds %d, want the 2 on live editions", got)
	}

	// And the ceiling, which cuts by count rather than by date and is the likelier place to
	// lose something somebody is looking at. A ceiling of zero would take everything that
	// was not spoken for.
	if _, err := s.CapItemsPerFeed(ctx, []string{feed}, 0+1); err != nil {
		t.Fatal(err)
	}
	if got := countItems(t, s, feed); got != 2 {
		t.Fatalf("after the ceiling the feed holds %d, want both live editions' articles", got)
	}

	// Named, so a failure says whose page lost its article rather than only how many are left.
	for i, who := range []string{"ada", "bob"} {
		var n int
		if err := s.derived.QueryRowContext(ctx,
			`SELECT count(*) FROM items WHERE id = ?`, items[i]).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s's page lost the article it was showing", who)
		}
	}

	// The one nobody was showing did go, or this proves nothing.
	var n int
	if err := s.derived.QueryRowContext(ctx,
		`SELECT count(*) FROM items WHERE id = ?`, items[2]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the article on nobody's page survived, so the sweep is not sweeping")
	}
}

// The protection is the *current* edition's, not any edition that ever existed — otherwise a
// page that has recomposed a hundred times would hold every article it had ever shown.
func TestASupersededEditionDoesNotHoldArticlesAlive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	feed := seedFeed(t, s, p.ID, "news", 24*time.Hour, 200, 201)

	var items []string
	rows, err := s.derived.QueryContext(ctx,
		`SELECT id FROM items WHERE feed_id = ? ORDER BY published_at DESC`, feed)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		items = append(items, id)
	}
	rows.Close()

	pages, err := s.Pages(ctx, p.ID)
	if err != nil || len(pages) == 0 {
		t.Fatal(err)
	}
	// Yesterday's edition, then today's. Only today's should hold anything alive.
	for i, id := range items {
		if _, err := s.AddEdition(ctx, pages[0], int64(i+1),
			[]Pick{{Item: &Item{ID: id}, Slot: SlotLead}}); err != nil {
			t.Fatal(err)
		}
	}

	// The sweep's order, which is what makes the unscoped guard mean "current".
	if _, err := s.PruneOldEditions(ctx); err != nil {
		t.Fatal(err)
	}
	byFeed, err := s.ItemRetentionByFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneItems(ctx, byFeed); err != nil {
		t.Fatal(err)
	}

	if got := countItems(t, s, feed); got != 1 {
		t.Errorf("the feed holds %d articles, want only the one the current edition shows", got)
	}
}
