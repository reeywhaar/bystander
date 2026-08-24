package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"bystander/internal/ids"
)

// Item is one article as its feed published it.
//
// Summary is sanitized HTML, and it is sanitized on the way in rather than on the way out,
// so every reader of this table gets the safe form by construction and a bug in a renderer
// cannot become an injection.
type Item struct {
	ID       string
	FeedID   string
	GUID     string
	Title    string
	Link     string
	Author   string
	Summary  string
	ImageURL string
	// ImageWidth and ImageHeight are the picture's real size, or zero when it has not been
	// measured — which is the ordinary case. See images.go.
	ImageWidth  int
	ImageHeight int
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
//
// A guid that moved is handled before that, by renameByLink. Between them, an article seen
// twice is recognised whichever of its two identifiers the publisher failed to keep still.
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

	unshared := unsharedLinks(items)

	added := 0
	for _, item := range items {
		// Named here rather than by whoever parsed it, because an article's id is derived
		// from the feed it belongs to and the parser does not always know that: discovery
		// parses a feed before there is a row for it. Naming articles there gave two
		// different feeds' articles the same ids, and the second feed's were then dropped
		// by the primary key — silently, one article at a time.
		item.ID = ids.Derive(ids.Article, item.FeedID, item.GUID)

		renamed, err := renameByLink(ctx, tx, item, unshared)
		if err != nil {
			return 0, err
		}
		if renamed {
			continue
		}

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

// unsharedLinks is the links this fetch used exactly once.
//
// The guard on renaming. A feed whose items all point at one page is a real thing — a
// summary feed, a badly generated one — and matching those on their link would fold the
// whole feed into a single article. Appearing once is the cheap evidence that a link
// identifies an article rather than the publication.
func unsharedLinks(items []*Item) map[string]bool {
	count := make(map[string]int, len(items))
	for _, item := range items {
		if item.Link != "" {
			count[item.Link]++
		}
	}
	out := make(map[string]bool, len(count))
	for link, n := range count {
		if n == 1 {
			out[link] = true
		}
	}
	return out
}

// renameByLink recognises an article whose guid has moved, and reports whether it did.
//
// An article's identity is the publisher's guid, and plenty of publishers cannot keep one
// still: theblueprint.ru appends the publication time to the permalink inside it, so editing
// an article changes the one field whose entire job is to stay the same. Every edit then
// arrives as a new article, and the same story sits on the page twice with the old headline
// beside the new one.
//
// So when a guid is one we have never seen but the link belongs to an article we already
// have, this is that article under a new name. The row is updated in place rather than
// inserted beside, which is the part that matters: the id stays, so being on somebody's page
// stays, and so does having been read. Adding a row would have quietly marked a story unread
// because its publisher fixed a typo.
//
// It cannot rescue an article whose guid *and* link both moved — a publisher who rewrites
// slugs is out of reach without guessing at titles, and guessing merges stories that are
// merely similar. Two identifiers is what a feed gives; using both is as far as this goes.
func renameByLink(ctx context.Context, tx *sql.Tx, item *Item, unshared map[string]bool) (bool, error) {
	if item.Link == "" || !unshared[item.Link] {
		return false, nil
	}

	var known int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM items WHERE feed_id = ? AND guid = ?`, item.FeedID, item.GUID).Scan(&known)
	if err == nil {
		// A guid we already have. Nothing has moved, and the insert below will ignore it.
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var (
		oldID   string
		oldGUID string
	)
	// The most recent, when a link somehow has more than one article behind it. That is the
	// version of the story a reader is looking at, and it is where the next edit should land.
	err = tx.QueryRowContext(ctx, `
		SELECT id, guid FROM items
		 WHERE feed_id = ? AND link = ?
		 ORDER BY published_at DESC, id DESC LIMIT 1`, item.FeedID, item.Link).Scan(&oldID, &oldGUID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// The row keeps the id it was first given, so everything pointing at it still does.
	// Handed back on the item too, so a caller that saved an article and then asks what its
	// id is gets the one in the table rather than the one this rename replaced.
	item.ID = oldID

	// published_at is deliberately left alone. A corrected headline is not a republication,
	// and letting the date follow the guid would shuffle an article back up the page every
	// time somebody fixed a comma in it.
	if _, err := tx.ExecContext(ctx, `
		UPDATE items
		   SET guid = ?, title = ?, author = ?, summary = ?, image_url = ?, fetched_at = ?
		 WHERE id = ?`,
		item.GUID, item.Title, item.Author, item.Summary, item.ImageURL,
		unix(item.FetchedAt), oldID); err != nil {
		return false, err
	}

	// The record of having shown it is keyed by the guid, so it has to follow. Without this
	// the rename would hand the article straight back to the sampler as something nobody has
	// seen — the duplicate, one cycle later.
	//
	// OR IGNORE and then a delete, because the new hash may already be there: an edition that
	// showed this article under its new name would already have written that row, and a plain
	// update would collide with it.
	if _, err := tx.ExecContext(ctx, `
		UPDATE OR IGNORE shown SET guid_hash = ? WHERE feed_id = ? AND guid_hash = ?`,
		GUIDHash(item.GUID), item.FeedID, GUIDHash(oldGUID)); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM shown WHERE feed_id = ? AND guid_hash = ?`,
		item.FeedID, GUIDHash(oldGUID)); err != nil {
		return false, err
	}

	// What somebody has read keeps its own copy of the headline, because that row has to
	// outlive the article it names. It is still a copy of *this* article though, so a
	// corrected headline belongs there too — otherwise the same story reads one way on the
	// page and another in the list of what has been read, which is the confusion this whole
	// function exists to remove.
	if _, err := tx.ExecContext(ctx,
		`UPDATE read_articles SET title = ? WHERE item_id = ?`, item.Title, oldID); err != nil {
		return false, err
	}
	return true, nil
}

const itemColumns = `id, feed_id, guid, title, link, author, summary, image_url, image_width, image_height, published_at, fetched_at`

func scanItem(row interface{ Scan(...any) error }) (*Item, error) {
	var (
		item      Item
		published int64
		fetched   int64
	)
	if err := row.Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link, &item.Author,
		&item.Summary, &item.ImageURL, &item.ImageWidth, &item.ImageHeight,
		&published, &fetched); err != nil {
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
// notOlderThan bounds how far back a candidate may be published, per feed — the window is
// the feed's, not the reader's. A missing or zero entry is no bound.
func (s *Store) Candidates(ctx context.Context, principalID string, feedIDs []string, perFeed int, notOlderThan map[string]time.Time) (map[string][]*Item, error) {
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
		if cutoff, ok := notOlderThan[feedID]; ok && !cutoff.IsZero() {
			since = unix(cutoff)
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
func (s *Store) Backfill(ctx context.Context, principalID string, feedIDs []string, perFeed int, notOlderThan map[string]time.Time, exclude map[string]bool) (map[string][]*Item, error) {
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
		if cutoff, ok := notOlderThan[feedID]; ok && !cutoff.IsZero() {
			since = unix(cutoff)
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
