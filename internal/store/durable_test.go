package store

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

func digest(t *testing.T, s *Store) []byte {
	t.Helper()
	sum, err := s.DurableDigest(t.Context())
	if err != nil {
		t.Fatalf("DurableDigest(): %v", err)
	}
	return sum
}

// The same database twice is the same digest, or nothing built on it means anything.
func TestTheDigestIsStable(t *testing.T) {
	s := testStore(t)
	if first, second := digest(t, s), digest(t, s); !bytes.Equal(first, second) {
		t.Errorf("two reads of one database differ:\n%x\n%x", first, second)
	}
}

// What somebody typed changes it; what the machine wrote down does not.
func TestWhatMovesTheDigest(t *testing.T) {
	ctx := t.Context()
	s := testStore(t)

	principal, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := s.UpsertFeed(ctx, "https://example.com/feed", "Example", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	before := digest(t, s)

	for _, tc := range []struct {
		what  string
		do    func() error
		moves bool
	}{
		{"a feed being polled", func() error {
			return s.RecordSuccess(ctx, feed.ID, "Example", "https://example.com",
				`W/"abc"`, "Mon, 01 Jan 2026 00:00:00 GMT", 200,
				30*time.Minute, time.Now().Add(30*time.Minute))
		}, false},
		{"a feed refusing to be polled", func() error {
			return s.RecordFailure(ctx, feed.ID, 503, "the server answered 503", "",
				time.Now().Add(time.Hour))
		}, false},
		{"work being queued", func() error {
			return s.Enqueue(ctx, "measure-image", "https://example.com/a.png", "a picture", "{}")
		}, false},

		{"a publisher renaming itself", func() error {
			return s.RecordSuccess(ctx, feed.ID, "Example, renamed", "https://example.com",
				`W/"abc"`, "", 200, 30*time.Minute, time.Now().Add(30*time.Minute))
		}, true},
		{"somebody following it", func() error {
			_, err := s.Subscribe(ctx, principal.ID, feed.ID, 50, 7*24*time.Hour, nil)
			return err
		}, true},
		{"somebody filing it under a tag", func() error {
			_, err := s.CreateTag(ctx, principal.ID, "News", "", 50)
			return err
		}, true},
	} {
		if err := tc.do(); err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		after := digest(t, s)
		if moved := !bytes.Equal(before, after); moved != tc.moves {
			t.Errorf("%s: moved the digest = %v, want %v", tc.what, moved, tc.moves)
		}
		before = after
	}
}

// A table nobody has classified counts, which is the safe direction to be wrong in.
//
// The list is of what to *ignore*, so anything added later is durable until somebody says
// otherwise — and being wrong that way costs a redundant backup rather than a change that
// waits forever to be noticed.
func TestAnUnknownTableCounts(t *testing.T) {
	ctx := t.Context()
	s := testStore(t)

	if _, err := s.main.ExecContext(ctx,
		`CREATE TABLE something_new (id TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	before := digest(t, s)

	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO something_new (id, value) VALUES ('a', 'b')`); err != nil {
		t.Fatal(err)
	}
	if after := digest(t, s); bytes.Equal(before, after) {
		t.Error("a row in a table nobody has classified did not register at all")
	}
}

// Two databases differing only in one value do not hash the same, whatever the value is.
//
// `quote` is what this rests on: it renders a SQL literal, so no value can be spelled the way
// another value's rendering is spelled. The pairs below are the ones a looser encoding gets
// wrong — a name containing the separator, a string that reads as a keyword, a number against
// the text of that number.
//
// Everything but the value under test is written explicitly and identically, or the databases
// would differ by their generated ids and this would pass whatever the digest did.
func TestNoTwoValuesRenderTheSame(t *testing.T) {
	ctx := t.Context()

	build := func(t *testing.T, names ...string) *Store {
		t.Helper()
		s := testStore(t)
		if _, err := s.main.ExecContext(ctx,
			`INSERT INTO principals (id, username, password_hash, role, created_at)
			 VALUES ('p_fixed', 'alice', 'x', 'user', 0)`); err != nil {
			t.Fatal(err)
		}
		for i, name := range names {
			if _, err := s.main.ExecContext(ctx,
				`INSERT INTO tags (id, principal_id, name, priority, created_at)
				 VALUES (?, 'p_fixed', ?, 50, 0)`,
				fmt.Sprintf("t_%d", i), name); err != nil {
				t.Fatal(err)
			}
		}
		return s
	}

	for _, tc := range []struct {
		what        string
		left, right []string
	}{
		{"a name holding the separator", []string{"a|b"}, []string{"a", "b"}},
		{"a name holding a quote", []string{"a'b"}, []string{"a''b"}},
		{"a name that reads as a keyword", []string{"NULL"}, []string{""}},
		{"a name that reads as a number", []string{"1"}, []string{"'1'"}},
		{"one name against no name", []string{"a"}, nil},
	} {
		t.Run(tc.what, func(t *testing.T) {
			left, right := build(t, tc.left...), build(t, tc.right...)
			if bytes.Equal(digest(t, left), digest(t, right)) {
				t.Errorf("%v and %v hash the same", tc.left, tc.right)
			}
		})
	}

	// The case that actually needs `quote` rather than any old rendering of the value:
	// nothing at all, against the empty string. Anything that substitutes a placeholder for
	// NULL — `ifnull(x, '')` being the obvious one — folds these together, and "this feed has
	// never been fetched" then reads as "it was fetched and said nothing".
	t.Run("nothing at all against the empty string", func(t *testing.T) {
		withValue := func(t *testing.T, insert string) *Store {
			t.Helper()
			s := testStore(t)
			if _, err := s.main.ExecContext(ctx,
				`CREATE TABLE nullable (id TEXT PRIMARY KEY, value TEXT)`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.main.ExecContext(ctx,
				`INSERT INTO nullable (id, value) VALUES ('a', `+insert+`)`); err != nil {
				t.Fatal(err)
			}
			return s
		}
		empty, absent := withValue(t, `''`), withValue(t, `NULL`)
		if bytes.Equal(digest(t, empty), digest(t, absent)) {
			t.Error("an empty string and a NULL hash the same")
		}
	})
}
