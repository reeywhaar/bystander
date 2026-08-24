package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"bystander/internal/store/migrations"
)

// testStore opens a store in a temp directory. A real file rather than :memory:, because
// WAL behaves differently in memory and WAL is the thing being relied on.
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// at pins the store's clock, so expiry is driven rather than slept through.
func at(t *testing.T, s *Store, when time.Time) {
	t.Helper()
	s.SetClock(func() time.Time { return when })
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	first, err := Open(dir)
	if err != nil {
		t.Fatalf("first Open(): %v", err)
	}
	first.Close()

	// Re-opening runs the migration loop again with nothing left to do, which is the
	// path every restart takes.
	second, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open(): %v", err)
	}
	defer second.Close()

	for _, db := range []struct {
		name string
		db   *sql.DB
		want int
	}{
		{MainFile, second.main, len(migrations.Main)},
		{DerivedFile, second.derived, len(migrations.Derived)},
	} {
		var version int
		if err := db.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			t.Fatalf("%s: read user_version: %v", db.name, err)
		}
		if version != db.want {
			t.Errorf("%s: user_version = %d, want %d", db.name, version, db.want)
		}
	}
}

func TestOpenCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	s.Close()
}

func TestOpenRefusesAFutureSchema(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	// A database written by a newer build than this one.
	if _, err := s.main.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	s.Close()

	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted a schema newer than this build understands")
	}
}

func TestPragmasTookEffect(t *testing.T) {
	s := testStore(t)
	for _, db := range []*sql.DB{s.main, s.derived} {
		var journal string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
			t.Fatalf("read journal_mode: %v", err)
		}
		if journal != "wal" {
			t.Errorf("journal_mode = %q, want wal", journal)
		}
		var fk int
		if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("read foreign_keys: %v", err)
		}
		if fk != 1 {
			t.Error("foreign_keys is off")
		}
	}
}

// The unique index is over ifnull(parent_id,”) precisely because SQLite treats NULLs as
// distinct in a UNIQUE constraint. Without it, two root tags could share a name.
func TestRootTagNamesAreUnique(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO principals (id, username, password_hash, role, created_at)
		 VALUES ('p_1', 'alice', 'x', 'user', 0)`); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	const insert = `INSERT INTO tags (id, principal_id, name, created_at) VALUES (?, 'p_1', 'Art', 0)`

	if _, err := s.main.ExecContext(ctx, insert, "t_1"); err != nil {
		t.Fatalf("insert first tag: %v", err)
	}
	if _, err := s.main.ExecContext(ctx, insert, "t_2"); err == nil {
		t.Fatal("a second root tag called Art was accepted")
	}
}

// Exactly one live edition per principal, enforced by the schema rather than by
// remembering to delete.
// Editions pile up and the newest is the page. This used to be one row per person, enforced by
// a unique index and maintained by deleting the old edition on the way in; the delete bought
// nothing and sat on the path of every compose.
func TestThePageIsTheNewestEdition(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const insert = `INSERT INTO editions (id, page_id, principal_id, generated_at, seed, size)
	                VALUES (?, 'pg_p_1', 'p_1', ?, 0, 10)`
	for _, ed := range []struct {
		id string
		at int64
	}{{"e_1", 100}, {"e_2", 200}, {"e_3", 150}} {
		if _, err := s.derived.ExecContext(ctx, insert, ed.id, ed.at); err != nil {
			t.Fatalf("insert %s: %v", ed.id, err)
		}
	}

	ed, _, err := s.CurrentEdition(ctx, "pg_p_1")
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	// Newest by when it was composed, not by when it was written.
	if ed.ID != "e_2" {
		t.Errorf("live edition = %s, want e_2", ed.ID)
	}

	n, err := s.PruneOldEditions(ctx)
	if err != nil {
		t.Fatalf("PruneOldEditions(): %v", err)
	}
	if n != 2 {
		t.Errorf("collected %d editions, want the 2 behind the newest", n)
	}
	if ed, _, err := s.CurrentEdition(ctx, "pg_p_1"); err != nil || ed.ID != "e_2" {
		t.Errorf("after collecting: %v, %v — the live edition should be untouched", ed, err)
	}
}

// Two composes inside one second, which is not the curiosity it looks like: generated_at is
// Unix seconds, and pressing "compose a page" twice does exactly this — as does saving a filter
// just after composing, since that recomposes.
//
// The live edition is the one written second. Ordering by id instead looks equivalent and is
// not: an id carries a millisecond timestamp and a random tail, so two minted in the same
// millisecond order at random, and the reader was shown the older edition about one time in
// ten. It surfaced as a scheduled-turn test that failed intermittently and passed on rerun.
func TestTwoEditionsInOneSecondKeepTheirOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const insert = `INSERT INTO editions (id, page_id, principal_id, generated_at, seed, size)
	                VALUES (?, 'pg_p_1', 'p_1', 500, 0, 10)`
	// Ids deliberately in the wrong order, which is what a random tail produces half the time.
	for _, id := range []string{"e_zzz_written_first", "e_aaa_written_second"} {
		if _, err := s.derived.ExecContext(ctx, insert, id); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	ed, _, err := s.CurrentEdition(ctx, "pg_p_1")
	if err != nil {
		t.Fatalf("CurrentEdition(): %v", err)
	}
	if ed.ID != "e_aaa_written_second" {
		t.Errorf("live edition = %s, want the one written second", ed.ID)
	}

	// And the sweep agrees with the reader, or it would collect the one being displayed.
	if n, err := s.PruneOldEditions(ctx); err != nil || n != 1 {
		t.Fatalf("PruneOldEditions() = %d, %v — want the first one collected", n, err)
	}
	if ed, _, err := s.CurrentEdition(ctx, "pg_p_1"); err != nil || ed.ID != "e_aaa_written_second" {
		t.Errorf("after collecting: %v, %v — the live edition should have survived", ed, err)
	}
}

// A person has one edition per page, and pruning must not treat another page's as superseded.
func TestEachPageKeepsItsOwnEdition(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const insert = `INSERT INTO editions (id, page_id, principal_id, generated_at, seed, size)
	                VALUES (?, ?, 'p_1', ?, 0, 10)`
	for _, ed := range []struct {
		id, page string
		at       int64
	}{{"e_1", "pg_p_1", 100}, {"e_2", "pg_p_1", 200}, {"e_3", "art", 50}} {
		if _, err := s.derived.ExecContext(ctx, insert, ed.id, ed.page, ed.at); err != nil {
			t.Fatalf("insert %s: %v", ed.id, err)
		}
	}

	if n, err := s.PruneOldEditions(ctx); err != nil || n != 1 {
		t.Fatalf("PruneOldEditions() = %d, %v — want only e_1 collected", n, err)
	}
	for page, want := range map[string]string{"pg_p_1": "e_2", "art": "e_3"} {
		ed, _, err := s.CurrentEdition(ctx, page)
		if err != nil {
			t.Fatalf("CurrentEdition(%s): %v", page, err)
		}
		if ed.ID != want {
			t.Errorf("%s shows %s, want %s", page, ed.ID, want)
		}
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	s := testStore(t)
	_, err := s.main.Exec(
		`INSERT INTO subscriptions (id, principal_id, feed_id, created_at) VALUES ('s_1','p_nope','f_nope',0)`)
	if err == nil {
		t.Fatal("a subscription referencing nothing was accepted")
	}
}
