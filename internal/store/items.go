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

// MinItemRetention is the floor on how long a feed's articles are kept, whatever its
// followers asked for. Below a month the pool gets thin enough that a page starts repeating
// itself.
//
// There is no matching ceiling. There was one, and it was wrong: how far back a page reaches
// bounds when an article was *published*, and pruning goes by when it was *fetched*, so a
// ceiling in years silently dropped the back catalogue of a feed whose articles were all
// written long ago. What bounds a feed instead is [MaxItemsPerFeed], a shelf length rather
// than a date. How long each feed is kept comes from its own followers — see
// ItemRetentionByFeed in settings.go.
//
// Items are a pool, not an archive: this is a front page, and anything worth keeping is worth
// keeping somewhere that is not here.
const MinItemRetention = 30 * 24 * time.Hour

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

// scanItem reads one item from a row.
//
// rest is scanned after the item's own columns, for a query that selects more than an item —
// so a join can still use [itemColumnsFrom] and this, and the column list and the order it is
// read in stay one thing rather than two that have to agree.
func scanItem(row interface{ Scan(...any) error }, rest ...any) (*Item, error) {
	var (
		item      Item
		published int64
		fetched   int64
	)
	into := append([]any{&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link, &item.Author,
		&item.Summary, &item.ImageURL, &item.ImageWidth, &item.ImageHeight,
		&published, &fetched}, rest...)
	if err := row.Scan(into...); err != nil {
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

// Queue is one feed's articles for a page being composed, in the order that page wants them.
//
// Three bands, in the order the page wants them, and the split is the whole of the sampler's
// input:
//
//   - Fresh — never shown on this page and never read by this person. What composing a page
//     is for.
//   - Unread — shown here before and never read. A repeat, but the good kind: it went past
//     without being dealt with, which is closer to new than to finished.
//   - Read — dealt with. Comes back greyed, and only when there is nothing else.
//
// Three bands rather than two independently built pools, and that is not tidying. They were
// two — candidates and backfill — each with its own tag buckets, and a feed missing from one
// of them silently vanished from the other: a page whose feeds had all been read through
// reached one feed out of five. A band is a position in a queue, not a separate collection.
type Queue struct {
	Fresh  []*Item
	Unread []*Item
	Read   []*Item
}

// Queues reads what each feed can offer the page being composed.
//
// "Shown" is per page and "read" is per person, and with one front page the second was a
// subset of the first — you could only have read what that page had shown you. With several
// it is not, and the two have to be tracked apart: an article read on a page of comics has
// never been shown on the page of everything, so it must not arrive there as though it were
// new and land greyed. It is a repeat, which is a band, not an exclusion — leaving it out of
// both was how five of the comics page's sixty-six articles became unreachable entirely.
//
// The banding is done in Go rather than in SQL because the shown table stores a digest of the
// guid and SQLite has no sha256: there is nothing to join on. Reading one feed's hashes and
// sorting the rows as they come back costs a set lookup per row, against a table that holds
// at most a few thousand entries per person.
//
// notOlderThan bounds how far back an article may be published, per feed — the window is the
// feed's, not the reader's. A missing or zero entry is no bound.
func (s *Store) Queues(ctx context.Context, pageID, principalID string, feedIDs []string, perFeed int,
	notOlderThan map[string]time.Time) (map[string]*Queue, error) {
	out := make(map[string]*Queue, len(feedIDs))

	for _, feedID := range feedIDs {
		seen, err := s.shownHashes(ctx, pageID, feedID)
		if err != nil {
			return nil, err
		}

		since := int64(0)
		if cutoff, ok := notOlderThan[feedID]; ok && !cutoff.IsZero() {
			since = unix(cutoff)
		}

		// Newest first, and more than perFeed because they are about to be sorted into three
		// bands and only one of them is wanted whole. Reading exactly perFeed would leave a
		// feed whose recent articles have all been shown looking as though it had nothing.
		rows, err := s.derived.QueryContext(ctx,
			`SELECT `+prefixed(itemColumns, "i")+`, r.item_id IS NOT NULL
			   FROM items i
			   LEFT JOIN read_articles r ON r.item_id = i.id AND r.principal_id = ?
			  WHERE i.feed_id = ? AND i.published_at >= ?
			  ORDER BY i.published_at DESC LIMIT ?`,
			principalID, feedID, since, perFeed*4)
		if err != nil {
			return nil, err
		}

		q := &Queue{}
		for rows.Next() {
			var wasRead bool
			item, err := scanItem(rows, &wasRead)
			if err != nil {
				rows.Close()
				return nil, err
			}
			// The rows arrive newest first, so appending keeps each band in that order.
			switch shown := seen[string(GUIDHash(item.GUID))]; {
			case wasRead:
				q.Read = append(q.Read, item)
			case shown:
				q.Unread = append(q.Unread, item)
			default:
				q.Fresh = append(q.Fresh, item)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}

		for _, band := range []*[]*Item{&q.Fresh, &q.Unread, &q.Read} {
			if len(*band) > perFeed {
				*band = (*band)[:perFeed]
			}
		}
		if len(q.Fresh)+len(q.Unread)+len(q.Read) > 0 {
			out[feedID] = q
		}
	}
	return out, nil
}

// shownHashes is what this principal has already been shown from one feed.
func (s *Store) shownHashes(ctx context.Context, pageID, feedID string) (map[string]bool, error) {
	rows, err := s.derived.QueryContext(ctx,
		`SELECT guid_hash FROM shown WHERE page_id = ? AND feed_id = ?`, pageID, feedID)
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
//
// The alternative is a second copy of the list that drifts from the first, and drifting is
// silent. The edition query kept its own copy, so when a picture's measured size was added to
// items it was added here and not there — and every article on every front page arrived with a
// measured picture reported as unmeasured. Nothing failed: the rows were read, the zeroes were
// the column default, and the page drew the shape it draws when it knows nothing.
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

// MaxItemsPerFeed is a ceiling on how many articles are held for any one feed.
//
// For most feeds this is a backstop that never fires: what decides how far back a feed is
// kept is what the people following it asked for — see [ItemRetentionByFeed]. For a feed
// somebody asked to reach back into without limit, this is the *only* bound there is, which
// is the honest way to offer "no limit": nothing is dropped for being old, and the shelf is
// still a finite length.
//
// A thousand, by publication date, whatever the window says. This is a judgement about
// reading rather than about storage: a front page is about what is going on, and the
// thousandth most recent thing one publisher has said is not something anybody is going to
// get to. A feed that put out a thousand articles yesterday has nothing to offer past them
// however far back somebody asked to reach.
//
// The consequence, stated rather than hidden: for a feed busy enough to reach a thousand
// inside its own window, this ceiling — not the window, and not the thirty-day floor
// [MinItemRetention] sets on age — is what decides how far back it goes. At ninety articles a
// day that is eleven days. Two bounds, and the tighter one wins; the floor still governs
// every feed quiet enough for a thousand to span it. Where this does bite the sweep names the
// feed, because a window being shortened by something other than the setting somebody chose
// should not be something they have to discover.
const MaxItemsPerFeed = 1000

// PruneItems drops articles past their feed's own retention, and everything belonging to a
// feed nobody follows, sparing anything a live edition still points at.
//
// retention is keyed by feed id — see [ItemRetentionByFeed]. A feed absent from it has no
// followers, so nothing is asking for any of its articles to be kept. Feeds live in the
// other database and no query can join across the boundary, which is why the answer is
// passed in rather than looked up.
func (s *Store) PruneItems(ctx context.Context, retention map[string]ItemRetention) (int64, error) {
	// Nobody follows anything, so nothing is worth keeping. Said explicitly rather than
	// left to "NOT IN ()", which is right by accident.
	if len(retention) == 0 {
		res, err := s.derived.ExecContext(ctx,
			`DELETE FROM items WHERE id NOT IN (SELECT item_id FROM edition_items)`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}

	now := s.Now()
	removed := int64(0)
	// One statement per feed. A CASE over every feed id would be one statement and a
	// query plan nobody can read; the sweep runs every few minutes over a few dozen feeds,
	// and each of these is an index seek.
	for feedID, keep := range retention {
		// Somebody asked to reach back into this feed without limit, so nothing here
		// drops any of it by age. MaxItemsPerFeed is what bounds it instead.
		if keep.Forever {
			continue
		}
		res, err := s.derived.ExecContext(ctx,
			`DELETE FROM items
			  WHERE feed_id = ? AND fetched_at < ?
			    AND id NOT IN (SELECT item_id FROM edition_items)`,
			feedID, unix(now.Add(-keep.For)))
		if err != nil {
			return removed, fmt.Errorf("prune articles of feed %s: %w", feedID, err)
		}
		n, _ := res.RowsAffected()
		removed += n
	}

	// And everything belonging to a feed nobody follows any more.
	followed := make([]string, 0, len(retention))
	for feedID := range retention {
		followed = append(followed, feedID)
	}
	args, placeholders := inList(followed)
	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM items
		  WHERE feed_id NOT IN (`+placeholders+`)
		    AND id NOT IN (SELECT item_id FROM edition_items)`, args...)
	if err != nil {
		return removed, err
	}
	n, _ := res.RowsAffected()
	return removed + n, nil
}

// CapItemsPerFeed holds each feed to at most max articles, newest first.
//
// The backstop under the age rule, and it reports per feed rather than in total on purpose:
// a feed appearing here is a feed whose window is being shortened by something other than
// the setting somebody chose, and that is worth a line in the log rather than a silent
// truncation. Returns only the feeds it actually took something from.
//
// Newest by publication rather than by when it was fetched, because that is the order a page
// draws in and the order a person means by "the last thousand". Anything a live edition
// points at is spared, like everywhere else: an article vanishing out from under a composed
// page is a hole in something somebody is reading.
func (s *Store) CapItemsPerFeed(ctx context.Context, feedIDs []string, max int) (map[string]int64, error) {
	if max <= 0 {
		return nil, nil
	}
	cut := map[string]int64{}
	for _, feedID := range feedIDs {
		res, err := s.derived.ExecContext(ctx,
			// LIMIT -1 OFFSET ? is SQLite's "everything past the first n".
			`DELETE FROM items
			  WHERE id IN (SELECT id FROM items WHERE feed_id = ?
			                ORDER BY published_at DESC, id DESC
			                LIMIT -1 OFFSET ?)
			    AND id NOT IN (SELECT item_id FROM edition_items)`,
			feedID, max)
		if err != nil {
			return cut, fmt.Errorf("cap articles of feed %s: %w", feedID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			cut[feedID] = n
		}
	}
	return cut, nil
}

// PruneShown holds each page's memory of one feed to its most recent [MaxItemsPerFeed]
// entries.
//
// By count rather than by age, and the difference is the point. This table is not a filter:
// an article a page has already shown is still perfectly eligible for the next edition, it
// merely waits behind everything the page has not offered yet — see the three bands in
// Queues. So a hash going early costs nothing but a place in a queue, on an article old
// enough that nothing was reaching it anyway.
//
// Age was the wrong measure twice over. It was a proxy for "the article is gone" that needed
// the whole retention calculation passed in to compute, and it stopped bounding anything at
// all once a feed could be kept without an age limit — a page showing sixty articles a day
// would then have written twenty thousand of these a year, for ever. A count needs nothing
// passed in and is bounded by construction: a feed holds at most MaxItemsPerFeed articles, so
// remembering more than that many shows from it is remembering things that can never come up
// again.
func (s *Store) PruneShown(ctx context.Context) (int64, error) {
	res, err := s.derived.ExecContext(ctx,
		// By the primary key, because this table is WITHOUT ROWID and so has no rowid to
		// address a row by.
		`DELETE FROM shown WHERE (page_id, feed_id, guid_hash) IN (
		   SELECT page_id, feed_id, guid_hash FROM (
		     SELECT page_id, feed_id, guid_hash, row_number() OVER (
		              PARTITION BY page_id, feed_id ORDER BY shown_at DESC) AS place
		       FROM shown)
		    WHERE place > ?)`, MaxItemsPerFeed)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
