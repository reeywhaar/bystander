package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"bystander/internal/store/migrations"
)

// openAt builds a database stopped at an earlier schema version, the way a deployment that
// has not been updated yet would have it.
func openAt(t *testing.T, dir, file string, list []migrations.Migration, version int) *sql.DB {
	t.Helper()

	db, err := open(filepath.Join(dir, file))
	if err != nil {
		t.Fatalf("open %s: %v", file, err)
	}
	// The other database is nil here: no migration that has shipped needs it, and a
	// future one that does will fail loudly rather than quietly reading nothing.
	if err := migrate(context.Background(), db, nil, file, list[:version]); err != nil {
		t.Fatalf("migrate %s to %d: %v", file, version, err)
	}
	return db
}

// Every version that has ever shipped has to be able to reach the current one. A migration
// that only works against a fresh database is not a migration.
func TestEveryEarlierVersionUpgrades(t *testing.T) {
	for _, set := range []struct {
		name       string
		file       string
		migrations []migrations.Migration
	}{
		{"main", MainFile, migrations.Main},
		{"derived", DerivedFile, migrations.Derived},
	} {
		for version := range len(set.migrations) {
			t.Run(fmt.Sprintf("%s/v%d", set.name, version), func(t *testing.T) {
				dir := t.TempDir()
				openAt(t, dir, set.file, set.migrations, version).Close()

				s, err := Open(dir)
				if err != nil {
					t.Fatalf("Open() on a database at version %d: %v", version, err)
				}
				defer s.Close()

				db := s.main
				if set.file == DerivedFile {
					db = s.derived
				}
				var got int
				if err := db.QueryRow("PRAGMA user_version").Scan(&got); err != nil {
					t.Fatalf("read user_version: %v", err)
				}
				if got != len(set.migrations) {
					t.Errorf("user_version = %d after upgrading from %d, want %d",
						got, version, len(set.migrations))
				}
			})
		}
	}
}

// The part that actually matters: an upgrade must not cost anybody their data.
func TestUpgradingKeepsWhatWasThere(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// A derived database as it stood before read_articles existed, with a page in it.
	derived := migrations.Derived
	previous := len(derived) - 1
	if previous < 1 {
		t.Skip("only one derived migration has shipped; nothing to upgrade from yet")
	}
	db := openAt(t, dir, DerivedFile, derived, previous)

	for _, stmt := range []string{
		`INSERT INTO items (id, feed_id, guid, title, link, published_at, fetched_at)
		 VALUES ('a_1', 'f_1', 'g1', 'A headline', 'https://example.com/1', 100, 100)`,
		`INSERT INTO editions (id, principal_id, generated_at, seed, size)
		 VALUES ('e_1', 'p_1', 100, 7, 10)`,
		`INSERT INTO edition_items (edition_id, item_id, rank, slot)
		 VALUES ('e_1', 'a_1', 0, 'lead')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed the old database: %v", err)
		}
	}

	// The shown table is seeded separately, because what it is keyed by has changed once
	// already — it was per person and is now per page — and this test seeds whatever the
	// schema looked like one migration ago. Hard-coding either column name makes this fail
	// on the day somebody adds a migration, with an error about a column rather than about
	// an upgrade losing data, which is the thing it is here to catch.
	var byPrincipal int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('shown') WHERE name = 'principal_id'`).
		Scan(&byPrincipal); err != nil {
		t.Fatalf("inspect shown: %v", err)
	}
	owner := "page_id"
	if byPrincipal == 1 {
		owner = "principal_id"
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO shown (`+owner+`, feed_id, guid_hash, shown_at) VALUES ('x_1', 'f_1', X'00', 100)`,
	); err != nil {
		t.Fatalf("seed the old database: %v", err)
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer s.Close()

	for _, check := range []struct {
		what  string
		query string
		want  int
	}{
		{"articles", `SELECT count(*) FROM items`, 1},
		{"pages", `SELECT count(*) FROM editions`, 1},
		{"articles on a page", `SELECT count(*) FROM edition_items`, 1},
		{"shown records", `SELECT count(*) FROM shown`, 1},
	} {
		var got int
		if err := s.derived.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", check.what, err)
		}
		if got != check.want {
			t.Errorf("%d %s survived the upgrade, want %d", got, check.what, check.want)
		}
	}

	// …and the table the upgrade added is there and works.
	if _, err := s.derived.ExecContext(ctx,
		`INSERT INTO read_articles (principal_id, item_id, feed_id, title, link, published_at, read_at)
		 VALUES ('p_1', 'a_1', 'f_1', 'A headline', 'https://example.com/1', 100, 200)`); err != nil {
		t.Fatalf("the upgraded database will not take a read article: %v", err)
	}
}

// A half-applied migration must leave the database at the last version that fully applied,
// not somewhere in between.
func TestAFailedMigrationLeavesTheVersionBehind(t *testing.T) {
	dir := t.TempDir()
	db, err := open(filepath.Join(dir, "broken.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	run := func(sql string) func(migrations.Context) error {
		return func(m migrations.Context) error {
			_, err := m.Tx.ExecContext(m.Ctx, sql)
			return err
		}
	}
	broken := []migrations.Migration{
		{Name: "00000000000001_test_fine", Up: run(`CREATE TABLE fine (id TEXT PRIMARY KEY);`)},
		// The first statement is valid and the second is not, which is the shape that
		// matters: the transaction has to take the first one back with it.
		{Name: "00000000000002_test_broken", Up: run(`CREATE TABLE also_fine (id TEXT PRIMARY KEY);
		 INSERT INTO no_such_table (id) VALUES ('x');`)},
	}
	if err := migrate(context.Background(), db, nil, "broken.db", broken); err == nil {
		t.Fatal("migrate() accepted a migration that cannot run")
	}

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("user_version = %d after the second migration failed, want 1", version)
	}
	// The failed migration's first statement must have gone with it.
	var count int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE name = 'also_fine'`).Scan(&count); err != nil {
		t.Fatalf("look for the rolled-back table: %v", err)
	}
	if count != 0 {
		t.Error("a table from the failed migration survived; the transaction did not roll back")
	}
}

// The first migration that carries data rather than only shape: the article window moved
// from being one setting per person to one per feed, and nobody's pages should have changed
// the day it landed.
func TestTheArticleWindowMovedOntoFeeds(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// A main database as it stood when the window was still a single setting.
	const before = 2
	if len(migrations.Main) <= before {
		t.Skip("the move has not shipped yet")
	}
	db := openAt(t, dir, MainFile, migrations.Main, before)

	for _, stmt := range []string{
		`INSERT INTO principals (id, username, password_hash, role, created_at)
		 VALUES ('p_1', 'alice', 'x', 'user', 0)`,
		// A month, rather than the default week — the point is that a choice survives.
		`INSERT INTO settings (principal_id, edition_interval, edition_size, max_article_age, next_edition_at)
		 VALUES ('p_1', 86400, 60, 2592000, 0)`,
		`INSERT INTO feeds (id, url, canonical_url, created_at)
		 VALUES ('f_1', 'https://a.example/rss', 'https://a.example/rss', 0),
		        ('f_2', 'https://b.example/rss', 'https://b.example/rss', 0)`,
		`INSERT INTO subscriptions (id, principal_id, feed_id, priority, created_at)
		 VALUES ('s_1', 'p_1', 'f_1', 50, 0), ('s_2', 'p_1', 'f_2', 90, 0)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed the old database: %v", err)
		}
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer s.Close()

	subs, err := s.ListSubscriptions(ctx, "p_1")
	if err != nil {
		t.Fatalf("ListSubscriptions(): %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("%d subscriptions survived, want 2", len(subs))
	}
	for _, sub := range subs {
		if sub.ArticleWindow != 30*24*time.Hour {
			t.Errorf("%s reaches back %s, want the month its owner had chosen", sub.ID, sub.ArticleWindow)
		}
	}

	// The setting it came from is gone, rather than left behind to disagree later.
	var columns int
	if err := s.main.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('settings') WHERE name = 'max_article_age'`).Scan(&columns); err != nil {
		t.Fatalf("inspect settings: %v", err)
	}
	if columns != 0 {
		t.Error("settings still carries max_article_age")
	}

	// And everything else about the settings row came through the table rebuild — and then
	// through becoming this person's main page, which is where those columns live now.
	page, err := s.PageByID(ctx, MainPageID("p_1"))
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}
	if page.EditionSize != 60 || page.EditionInterval != 24*time.Hour {
		t.Errorf("page = %+v, want what was there before", page)
	}
}
