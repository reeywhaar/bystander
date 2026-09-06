package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// volatile is what the machine writes down in main.db as it works.
//
// Not "unimportant" — all of it travels in every backup, because a backup is the whole file.
// What this list decides is narrower: whether a change to it is on its own a reason to send
// another copy somewhere.
//
// It is a denylist rather than an allowlist, and that is the safe direction. A table added
// later counts as durable without anybody remembering to say so, and the cost of being wrong
// that way is a redundant upload rather than a change that waits forever to be noticed.
//
// A nil entry means the whole table.
var volatile = map[string][]string{
	// The cause of the problem this exists for. Every fetch of every feed rewrites all of
	// these, and on a real instance of fifty-five feeds that is about twelve writes an hour —
	// so roughly two five-minute windows in three found main.db "changed" and sent a copy of
	// the entire database because a publisher had been asked whether it had anything new.
	//
	// `title` and `site_url` are deliberately *not* here. A fetch writes them too, but with
	// the same value each time — and on the fetch where the value differs, the publisher has
	// renamed itself, which is a change worth keeping.
	"feeds": {
		"etag", "last_modified",
		"last_fetch_at", "last_success_at", "last_status",
		"last_error", "last_error_body", "failure_count",
		// Recomputed from the feed's own cadence after every fetch, so it drifts by a
		// minute or two at a time. See feeds.Cadence.
		"fetch_interval", "next_fetch_at",
	},

	// Work in flight. Rows appear and vanish as the runner gets to them — the image measurer
	// alone queues two hundred at a time — and none of it is a decision anybody made.
	"jobs": nil,

	// A session's identity counts, so signing in and signing out are both changes worth
	// keeping. Where it was last used is not: that slides forward on its own every hour a
	// tab is left open. See session.Refresh.
	"sessions": {"last_seen_at", "expires_at", "last_ip", "last_user_agent"},

	// When the next edition is due, which the scheduler moves every time it composes one.
	// What the page is *set* to — its interval, its size, its filters — is not here.
	"pages": {"next_edition_at"},
}

// DurableDigest is a fingerprint of what somebody typed into main.db.
//
// It answers the only question a backup has to ask — has anything changed that is worth
// keeping — and it answers it about the *content* rather than about the file. The file is the
// wrong thing to ask: it moves whenever a feed is polled, which is constantly and means
// nothing, and it is what made an instance send a full backup every ten minutes for a week.
//
// Cheaper than the snapshot it replaces, as well as more accurate. Hashing the file meant
// `VACUUM INTO` writing a whole copy of the database every five minutes purely to be hashed
// and thrown away; this reads the handful of small tables somebody actually edits.
func (s *Store) DurableDigest(ctx context.Context) ([]byte, error) {
	tables, err := s.tables(ctx)
	if err != nil {
		return nil, err
	}

	sum := sha256.New()
	for _, table := range tables {
		skip, listed := volatile[table]
		if listed && skip == nil {
			continue
		}
		columns, err := s.durableColumns(ctx, table, skip)
		if err != nil {
			return nil, err
		}
		if len(columns) == 0 {
			continue
		}
		// The shape goes in as well as the contents, so a column being dropped or renamed
		// registers as a change even where every remaining value is identical.
		fmt.Fprintf(sum, "\ntable %s(%s)\n", table, strings.Join(columns, ","))

		if err := s.hashRows(ctx, table, columns, sum); err != nil {
			return nil, err
		}
	}
	return sum.Sum(nil), nil
}

// tables is every table in main.db, in a settled order.
func (s *Store) tables(ctx context.Context) ([]string, error) {
	rows, err := s.main.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// durableColumns is one table's columns with the volatile ones removed, in declared order.
func (s *Store) durableColumns(ctx context.Context, table string, skip []string) ([]string, error) {
	drop := make(map[string]bool, len(skip))
	for _, name := range skip {
		drop[name] = true
	}

	// The table name cannot be a bind parameter in a PRAGMA, and does not come from anywhere
	// a stranger can reach: it was read out of sqlite_master a moment ago.
	rows, err := s.main.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, kind       string
			deflt            any
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &deflt, &pk); err != nil {
			return nil, err
		}
		if !drop[name] {
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// hashRows folds one table's durable content into sum.
//
// `quote` renders every type as the SQL literal for it, which gives one canonical text form for
// text, numbers, blobs and NULL alike — and means the row can be scanned as strings without
// asking each column what it is. It is also what keeps the encoding unambiguous: two different
// databases cannot render to the same bytes, whatever anybody types into a name. Ordered by
// those same expressions, so the digest does not depend on the order SQLite happens to return
// rows in.
func (s *Store) hashRows(ctx context.Context, table string, columns []string, sum interface{ Write([]byte) (int, error) }) error {
	quoted := make([]string, len(columns))
	order := make([]string, len(columns))
	for i, name := range columns {
		quoted[i] = fmt.Sprintf("quote(%q)", name)
		order[i] = fmt.Sprint(i + 1)
	}
	query := fmt.Sprintf("SELECT %s FROM %q ORDER BY %s",
		strings.Join(quoted, ", "), table, strings.Join(order, ", "))

	rows, err := s.main.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	values := make([]string, len(columns))
	into := make([]any, len(columns))
	for i := range values {
		into[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(into...); err != nil {
			return err
		}
		for _, value := range values {
			// `quote` is what makes this unambiguous rather than the separator, which is
			// only here to be read. It renders a SQL literal: text single-quoted with any
			// internal quote doubled, blobs as X'…', numbers bare, NULL as a bare NULL. So
			// no value can be spelled the way another value's rendering is spelled — a tag
			// named "a|b" comes through as 'a|b' where two tags named "a" and "b" come
			// through as 'a' and 'b', and the string "NULL" is 'NULL' where an actual NULL
			// is NULL.
			fmt.Fprintf(sum, "%s|", value)
		}
		fmt.Fprint(sum, "\n")
	}
	return rows.Err()
}
