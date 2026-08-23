package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

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
