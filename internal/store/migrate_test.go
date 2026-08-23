package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// released is every migration that has ever shipped, by filename and by the sha256 of its
// contents.
//
// This is what turns "never edit a released migration" from a comment into something the
// build enforces. Every deployment past a given file has already recorded it as applied and
// will skip it forever, so editing one silently leaves the schema in front of the code
// different from the schema in the file — on those databases and no others, which is the
// worst kind of difference to go looking for.
//
// Adding a migration means adding its file here. Changing a hash that is already here means
// you have edited history; add a new file instead.
//
// The hash is over the trimmed contents, so reindenting a file or moving it between forms
// is not an edit. What it catches is a change to the SQL.
var released = map[string][]struct{ name, sha string }{
	"main": {
		{"20260823030000_initial_schema.sql", "95dd6ba07a343da0b310350cfd0144dc020051c21a33e97eb8f8a4406aa5f1fb"},
		{"20260823061500_article_window.sql", "ee4e1466b82dc33b35a0708c5fcce0a84da1481bf7e113182133cd05560b0dc1"},
	},
	"derived": {
		{"20260823030000_initial_schema.sql", "605d7e1b5ffe21228032e339d60fccef96462f63443977ac0953aaae4e55e83a"},
		{"20260823051000_read_articles.sql", "5cb8a4d336c72de5b0161f0aea46774eadc1d127232a9abe3cdb62735b171727"},
	},
}

func migrationSets() map[string][]Migration {
	return map[string][]Migration{"main": mainMigrations(), "derived": derivedMigrations()}
}

// short trims a hash for a message without assuming it is a hash.
func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func hashOf(m Migration) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(m.SQL)))
	return hex.EncodeToString(sum[:])
}

func TestMigrationsAreAppendOnly(t *testing.T) {
	for name, list := range migrationSets() {
		expected := released[name]

		if len(list) < len(expected) {
			t.Errorf("%s has %d migrations but %d have shipped; a released migration cannot be removed",
				name, len(list), len(expected))
			continue
		}

		for i, want := range expected {
			got := list[i]
			if got.Name != want.name {
				t.Errorf("%s migration %d is %q, but %q shipped in that position;\n"+
					"migrations are applied in filename order and that order is the schema's history",
					name, i+1, got.Name, want.name)
				continue
			}
			if sha := hashOf(got); sha != want.sha {
				t.Errorf("%s/%s has been edited since it shipped.\n"+
					"Databases that already applied it will never see the change. Add a new\n"+
					"migration instead. (was %s, now %s)", name, got.Name, short(want.sha), short(sha))
			}
		}

		if len(list) > len(expected) {
			for i := len(expected); i < len(list); i++ {
				t.Logf("%s has a new migration; add it to `released` above:\n\t\t{%q, %q},",
					name, list[i].Name, hashOf(list[i]))
			}
			t.Errorf("%s has %d unreleased migrations; record them above",
				name, len(list)-len(expected))
		}
	}
}

// Filenames carry the order, so they have to be orderable and readable.
func TestMigrationsAreNamedForOrdering(t *testing.T) {
	pattern := regexp.MustCompile(`^\d{14}_[a-z0-9_]+\.sql$`)
	for name, list := range migrationSets() {
		seen := map[string]bool{}
		for _, m := range list {
			if !pattern.MatchString(m.Name) {
				t.Errorf("%s/%s is not <14-digit timestamp>_<snake_case_name>.sql", name, m.Name)
			}
			stamp := strings.SplitN(m.Name, "_", 2)[0]
			if seen[stamp] {
				t.Errorf("%s/%s shares a timestamp with another migration; the order is ambiguous",
					name, m.Name)
			}
			seen[stamp] = true
			if strings.TrimSpace(m.SQL) == "" {
				t.Errorf("%s/%s is empty", name, m.Name)
			}
		}
	}
}

// openAt builds a database stopped at an earlier schema version, the way a deployment that
// has not been updated yet would have it.
func openAt(t *testing.T, dir, file string, migrations []Migration, version int) *sql.DB {
	t.Helper()

	db, err := open(filepath.Join(dir, file))
	if err != nil {
		t.Fatalf("open %s: %v", file, err)
	}
	if err := migrate(context.Background(), db, file, migrations[:version]); err != nil {
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
		migrations []Migration
	}{
		{"main", MainFile, mainMigrations()},
		{"derived", DerivedFile, derivedMigrations()},
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
	derived := derivedMigrations()
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
		`INSERT INTO shown (principal_id, feed_id, guid_hash, shown_at)
		 VALUES ('p_1', 'f_1', X'00', 100)`,
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

	broken := []Migration{
		{Name: "0001_fine.sql", SQL: `CREATE TABLE fine (id TEXT PRIMARY KEY);`},
		// The first statement is valid and the second is not, which is the shape that
		// matters: the transaction has to take the first one back with it.
		{Name: "0002_broken.sql", SQL: `CREATE TABLE also_fine (id TEXT PRIMARY KEY);
		 INSERT INTO no_such_table (id) VALUES ('x');`},
	}
	if err := migrate(context.Background(), db, "broken.db", broken); err == nil {
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
