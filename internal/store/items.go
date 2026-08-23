package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Item is one article as its feed published it.
//
// Summary is sanitized HTML, and it is sanitized on the way in rather than on the way out,
// so every reader of this table gets the safe form by construction and a bug in a renderer
// cannot become an injection.
type Item struct {
	ID          string
	FeedID      string
	GUID        string
	Title       string
	Link        string
	Author      string
	Summary     string
	ImageURL    string
	PublishedAt time.Time
	FetchedAt   time.Time
}

// Retention. Items are a pool, not an archive: this is a front page, and anything worth
// keeping is worth keeping somewhere that is not here.
//
// It is not a fixed number, because it cannot be. Somebody who has asked to see a year of
// articles needs a year of articles kept; somebody who wants a day does not. So the bounds
// are here and EffectiveItemRetention picks between them from what people have actually
// chosen — see settings.go.
const (
	// MinItemRetention is the floor, whatever anybody has asked for. Below a month the
	// pool gets thin enough that a page starts repeating itself.
	MinItemRetention = 30 * 24 * time.Hour

	// MaxItemRetention is the ceiling, and what "no limit" means in practice. Unbounded
	// growth is not a setting anybody meant to choose.
	MaxItemRetention = 365 * 24 * time.Hour
)

// shownMultiple keeps the record that an article was shown outliving the article itself.
// If it did not, a long-dormant feed could resurface something already read.
const shownMultiple = 3

// SaveItems writes what a fetch produced and reports how many were new.
//
// INSERT OR IGNORE against the unique (feed_id, guid) index is what makes re-fetching
// idempotent: a feed that republishes its whole window every hour produces no duplicates
// and no work.
func (s *Store) SaveItems(ctx context.Context, items []*Item) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	tx, err := s.derived.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO items
		   (id, feed_id, guid, title, link, author, summary, image_url, published_at, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	added := 0
	for _, item := range items {
		res, err := stmt.ExecContext(ctx, item.ID, item.FeedID, item.GUID, item.Title, item.Link,
			item.Author, item.Summary, item.ImageURL, unix(item.PublishedAt), unix(item.FetchedAt))
		if err != nil {
			return 0, fmt.Errorf("save item %q: %w", item.GUID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

const itemColumns = `id, feed_id, guid, title, link, author, summary, image_url, published_at, fetched_at`

func scanItem(row interface{ Scan(...any) error }) (*Item, error) {
	var (
		item      Item
		published int64
		fetched   int64
	)
	if err := row.Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link, &item.Author,
		&item.Summary, &item.ImageURL, &published, &fetched); err != nil {
		return nil, err
	}
	item.PublishedAt = time.Unix(published, 0).UTC()
	item.FetchedAt = time.Unix(fetched, 0).UTC()
	return &item, nil
}

// ItemByID returns one article.
func (s *Store) ItemByID(ctx context.Context, id string) (*Item, error) {
	item, err := scanItem(s.derived.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no article %s", id)
	}
	return item, err
}

// Candidates returns, per feed, the newest articles this principal has not been shown.
//
// The exclusion is done in Go rather than in SQL because the shown table stores a digest
// of the guid and SQLite has no sha256: there is nothing to join on. Reading one feed's
// hashes and filtering as the rows come back costs a set lookup per row, against a table
// that holds at most a few thousand entries per person.
// notOlderThan bounds how far back a candidate may be published. A zero time is no bound.
func (s *Store) Candidates(ctx context.Context, principalID string, feedIDs []string, perFeed int, notOlderThan time.Time) (map[string][]*Item, error) {
	out := make(map[string][]*Item, len(feedIDs))
	for _, feedID := range feedIDs {
		seen, err := s.shownHashes(ctx, principalID, feedID)
		if err != nil {
			return nil, err
		}

		// Reading more than perFeed, because some of what comes back will be filtered
		// out here: taking exactly perFeed would leave a feed whose recent articles have
		// all been shown looking empty when it is not.
		// The age bound is in the query rather than in the loop below: an article too old
		// to appear is not a candidate that gets filtered, it is not a candidate — and
		// leaving it to the loop would let a feed's whole window be spent on articles
		// that were never eligible.
		since := int64(0)
		if !notOlderThan.IsZero() {
			since = unix(notOlderThan)
		}
		rows, err := s.derived.QueryContext(ctx,
			`SELECT `+itemColumns+` FROM items
			  WHERE feed_id = ? AND published_at >= ?
			  ORDER BY published_at DESC LIMIT ?`,
			feedID, since, perFeed*4)
		if err != nil {
			return nil, err
		}

		var items []*Item
		for rows.Next() {
			item, err := scanItem(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			if seen[string(GUIDHash(item.GUID))] {
				continue
			}
			items = append(items, item)
			if len(items) == perFeed {
				break
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out[feedID] = items
		}
	}
	return out, nil
}

// Backfill returns, per feed, the newest articles this principal has already been shown.
//
// The complement of Candidates, and the reason it exists: a page with room left over and
// nothing fresh to put in it looks broken rather than honest. Filling the rest with things
// already seen keeps the shape of a front page, and costs nothing — those articles were
// going to be pruned unread either way.
//
// exclude is what the page already holds, so an article drawn fresh is not offered back.
func (s *Store) Backfill(ctx context.Context, principalID string, feedIDs []string, perFeed int, notOlderThan time.Time, exclude map[string]bool) (map[string][]*Item, error) {
	out := make(map[string][]*Item, len(feedIDs))

	for _, feedID := range feedIDs {
		seen, err := s.shownHashes(ctx, principalID, feedID)
		if err != nil {
			return nil, err
		}
		if len(seen) == 0 {
			continue
		}

		since := int64(0)
		if !notOlderThan.IsZero() {
			since = unix(notOlderThan)
		}
		// Things seen but never read come first.
		//
		// Both are repeats, but they are not equally good ones: an article that went past
		// unread is closer to new than one somebody has already finished with. read_articles
		// lives in this same database, so the preference is a join rather than a second
		// query and a sort in Go.
		rows, err := s.derived.QueryContext(ctx,
			`SELECT `+prefixed(itemColumns, "i")+`
			   FROM items i
			   LEFT JOIN read_articles r ON r.item_id = i.id AND r.principal_id = ?
			  WHERE i.feed_id = ? AND i.published_at >= ?
			  ORDER BY (r.item_id IS NOT NULL), i.published_at DESC
			  LIMIT ?`,
			principalID, feedID, since, perFeed*4)
		if err != nil {
			return nil, err
		}

		var items []*Item
		for rows.Next() {
			item, err := scanItem(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			// Only what has been shown, and not what is already on the page.
			if !seen[string(GUIDHash(item.GUID))] || exclude[item.ID] {
				continue
			}
			items = append(items, item)
			if len(items) == perFeed {
				break
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out[feedID] = items
		}
	}
	return out, nil
}

// shownHashes is what this principal has already been shown from one feed.
func (s *Store) shownHashes(ctx context.Context, principalID, feedID string) (map[string]bool, error) {
	rows, err := s.derived.QueryContext(ctx,
		`SELECT guid_hash FROM shown WHERE principal_id = ? AND feed_id = ?`, principalID, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	for rows.Next() {
		var hash []byte
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		seen[string(hash)] = true
	}
	return seen, rows.Err()
}

// prefixed qualifies a column list for a join, so "id, feed_id" becomes "i.id, i.feed_id".
// The alternative is a second copy of the list that drifts from the first.
func prefixed(columns, table string) string {
	parts := strings.Split(columns, ", ")
	for i, part := range parts {
		parts[i] = table + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

// GUIDHash is how an already-shown article is remembered: a truncated digest rather than
// the guid itself, because the shown table exists in every edition's shadow and a guid can
// be a whole URL.
func GUIDHash(guid string) []byte {
	sum := sha256.Sum256([]byte(guid))
	return sum[:16]
}

// PruneItems drops articles past retention and articles belonging to feeds that no longer
// exist, sparing anything a live edition still points at.
//
// Feeds are in the other database, so which ones exist has to be passed in: no constraint
// can cross the boundary and no query can join across it.
func (s *Store) PruneItems(ctx context.Context, liveFeedIDs []string, retention time.Duration) (int64, error) {
	cutoff := unix(s.Now().Add(-retention))

	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM items
		  WHERE fetched_at < ?
		    AND id NOT IN (SELECT item_id FROM edition_items)`, cutoff)
	if err != nil {
		return 0, err
	}
	removed, _ := res.RowsAffected()

	// An empty list means no feeds at all, which would make "not in ()" delete
	// everything — correct, but only by accident. Say it explicitly.
	if len(liveFeedIDs) == 0 {
		res, err := s.derived.ExecContext(ctx, `DELETE FROM items WHERE id NOT IN (SELECT item_id FROM edition_items)`)
		if err != nil {
			return removed, err
		}
		n, _ := res.RowsAffected()
		return removed + n, nil
	}

	args := make([]any, 0, len(liveFeedIDs))
	for _, id := range liveFeedIDs {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(liveFeedIDs)), ",")
	res, err = s.derived.ExecContext(ctx,
		`DELETE FROM items
		  WHERE feed_id NOT IN (`+placeholders+`)
		    AND id NOT IN (SELECT item_id FROM edition_items)`, args...)
	if err != nil {
		return removed, err
	}
	n, _ := res.RowsAffected()
	return removed + n, nil
}

// PruneShown drops the record of articles shown long enough ago that their items are gone
// too. Kept for a multiple of however long articles are being kept, so the record always
// outlives what it refers to.
func (s *Store) PruneShown(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM shown WHERE shown_at < ?`, unix(s.Now().Add(-retention*shownMultiple)))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
