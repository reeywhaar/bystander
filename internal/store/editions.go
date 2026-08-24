package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"bystander/internal/ids"
)

// Slot is how prominently an article is laid out, and how much of the page's sixteen tracks
// it takes: lead all of them, wide twelve, feature eight, standard and brief four.
//
// Every width is a multiple of the narrowest, so whatever a row has left over is a width
// something else can fill — the page can be irregular without ever stranding a gap nothing
// fits. Twelve is what makes it irregular at all: a row holding one has four tracks left,
// which only a single column can take, so the grid has to reach past the next article to
// find one. See web/src/lib/voice.ts for what this is in service of.
//
// Decided at generation time and stored, so the page is identical on every reload until
// the next edition replaces it. The type lives here because the schema's CHECK constraint
// holds the same four values.
type Slot string

const (
	SlotLead     Slot = "lead"
	SlotWide     Slot = "wide"
	SlotFeature  Slot = "feature"
	SlotStandard Slot = "standard"
	SlotBrief    Slot = "brief"
)

// Edition is one person's live front page.
type Edition struct {
	ID     string
	PageID string
	// PrincipalID is whose page this is, kept beside PageID because derived.db cannot join to
	// main.db to work it out. See AddEdition.
	PrincipalID string
	GeneratedAt time.Time
	Seed        int64
	Size        int
}

// EditionItem is an article in its place on the page.
type EditionItem struct {
	*Item
	Rank   int
	Slot   Slot
	ReadAt time.Time // zero until somebody marks it
}

// Read reports whether this article has been marked.
func (e *EditionItem) Read() bool { return !e.ReadAt.IsZero() }

// Pick is one selected article, with where it goes.
type Pick struct {
	Item *Item
	Rank int
	Slot Slot
}

// currentEditions is every page's newest edition, as a CTE the queries below build on.
//
// Written once because three different questions need it and they must agree: which edition a
// page is showing, which editions an article should be marked read across, and which editions
// still count as displaying an article. A second spelling of "the live one" that drifted from
// this would show up as read marks landing on a page nobody is looking at.
const currentEditions = `
WITH ranked AS (
    SELECT id, page_id, principal_id,
           row_number() OVER (PARTITION BY page_id ORDER BY generated_at DESC, rowid DESC) AS rn
      FROM editions
), current AS (
    SELECT id, page_id, principal_id FROM ranked WHERE rn = 1
)`

// AddEdition writes a new edition for a page. It becomes the live one by being the newest.
//
// Nothing is deleted here, and the old edition is left where it is. That used to be the other
// way round — one edition per page, enforced by a unique index, with the previous one deleted
// inside this transaction — and the deleting bought nothing. The live edition is a question
// with an obvious answer ("the newest") and answering it by keeping the table to one row put a
// delete on the path of every compose, where it could fail and take the compose with it.
//
// So editions accumulate and the sweep collects them. Between sweeps a page has a short history
// behind it, which costs some rows and is otherwise invisible: everything that reads a page
// reads the newest.
//
// The principal is written beside the page, denormalised on purpose. derived.db cannot join to
// main.db to find out whose page this is, and marking an article read has to reach every one of
// a person's live editions at once. Both columns are set here, together, from one Page.
func (s *Store) AddEdition(ctx context.Context, page *Page, seed int64, picks []Pick) (*Edition, error) {
	now := s.Now()
	ed := &Edition{
		ID:          ids.New(ids.Edition),
		PageID:      page.ID,
		PrincipalID: page.PrincipalID,
		GeneratedAt: now,
		Seed:        seed,
		Size:        page.EditionSize,
	}
	principalID := page.PrincipalID

	tx, err := s.derived.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO editions (id, page_id, principal_id, generated_at, seed, size)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ed.ID, page.ID, principalID, unix(now), seed, ed.Size); err != nil {
		return nil, fmt.Errorf("create edition: %w", err)
	}

	// read_at is carried over from the month-long record rather than left null.
	//
	// It only ever fires for an article being shown again — a fresh one has never been on
	// a page, so nobody can have read it. Without this, an article somebody read last week
	// would come back looking new, which is the one thing a page that repeats itself must
	// not do.
	item, err := tx.PrepareContext(ctx,
		`INSERT INTO edition_items (edition_id, item_id, rank, slot, read_at)
		 VALUES (?, ?, ?, ?,
		   (SELECT read_at FROM read_articles WHERE principal_id = ? AND item_id = ?))`)
	if err != nil {
		return nil, err
	}
	defer item.Close()

	// INSERT OR REPLACE rather than INSERT: an article can legitimately be shown again
	// after its previous record was pruned, and re-stamping the date is exactly right.
	shown, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO shown (page_id, feed_id, guid_hash, shown_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer shown.Close()

	for _, pick := range picks {
		if _, err := item.ExecContext(ctx, ed.ID, pick.Item.ID, pick.Rank, string(pick.Slot),
			principalID, pick.Item.ID); err != nil {
			return nil, fmt.Errorf("place article %s: %w", pick.Item.ID, err)
		}
		if _, err := shown.ExecContext(ctx, page.ID, pick.Item.FeedID, GUIDHash(pick.Item.GUID), unix(now)); err != nil {
			return nil, fmt.Errorf("record article %s as shown: %w", pick.Item.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ed, nil
}

// CurrentEdition returns one page's live edition, or ErrNotFound before its first generation.
//
// The newest, because that is what "live" means now that editions accumulate.
//
// Ties are broken by rowid, which is insertion order — and the tie is not the rare curiosity it
// looks like. generated_at is Unix *seconds*, so two composes in the same second tie, and
// pressing "compose a page" twice or saving a filter just after composing does exactly that.
// Breaking the tie on the id instead looks equivalent and is not: ids carry a millisecond
// timestamp and a random tail, so two minted in the same millisecond order at random — which
// meant the reader was shown the *older* of the two editions about one time in ten. It was a
// test failing intermittently that said so.
func (s *Store) CurrentEdition(ctx context.Context, pageID string) (*Edition, []*EditionItem, error) {
	var (
		ed        Edition
		generated int64
	)
	err := s.derived.QueryRowContext(ctx,
		`SELECT id, page_id, principal_id, generated_at, seed, size
		   FROM editions WHERE page_id = ?
		  ORDER BY generated_at DESC, rowid DESC LIMIT 1`,
		pageID).Scan(&ed.ID, &ed.PageID, &ed.PrincipalID, &generated, &ed.Seed, &ed.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, NotFound("no page has been generated yet")
	}
	if err != nil {
		return nil, nil, err
	}
	ed.GeneratedAt = time.Unix(generated, 0).UTC()

	// The item's own columns come from the one list in items.go rather than a copy here. A
	// copy is what this was, and it silently went stale — see [itemColumnsFrom].
	rows, err := s.derived.QueryContext(ctx,
		`SELECT `+prefixed(itemColumns, "i")+`, e.rank, e.slot, e.read_at
		   FROM edition_items e JOIN items i ON i.id = e.item_id
		  WHERE e.edition_id = ?
		  ORDER BY e.rank`, ed.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var out []*EditionItem
	for rows.Next() {
		var (
			slot  string
			read  sql.NullInt64
			entry EditionItem
		)
		item, err := scanItem(rows, &entry.Rank, &slot, &read)
		if err != nil {
			return nil, nil, err
		}
		entry.Item = item
		entry.Slot = Slot(slot)
		entry.ReadAt = timeFrom(read)
		out = append(out, &entry)
	}
	return &ed, out, rows.Err()
}

// ReadListLimit is how much of somebody's reading the list shows.
//
// The record itself is kept until they stop following the feed — see the migration — but the
// list is called Recently read and five hundred of them is recent by any measure. Without a
// bound this grows without one: a reader getting through fifty articles a day has eighteen
// thousand rows after a year, and a screen that renders all of them is a screen that stops
// opening.
const ReadListLimit = 500

// ReadArticle is something this person has read, as remembered after its page is gone.
type ReadArticle struct {
	ItemID      string
	FeedID      string
	Title       string
	Link        string
	PublishedAt time.Time
	ReadAt      time.Time
}

// SetRead marks an article read or unread on this principal's live page.
//
// Scoped through the editions table rather than trusting the item id: an item id is shared
// by everybody who follows that feed, so without the join one person could mark another's
// page.
//
// Marking also writes to read_articles, in the same transaction, and unmarking removes it
// again. Those two records answer different questions and have different lifetimes — the
// mark greys a card on this page and dies with it; the record outlives the page by a month
// — but they must never disagree about whether something was read.
func (s *Store) SetRead(ctx context.Context, principalID, itemID string, read bool) error {
	now := s.Now()

	tx, err := s.derived.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Every one of this person's live editions, not just the page they were looking at.
	//
	// Reading is a fact about a person and an article. The same article can sit on a page of
	// everything and on a page filtered to one tag at the same time, and seeing it unread on the
	// next tab having just read it reads as a bug rather than as a distinction anybody wanted.
	res, err := tx.ExecContext(ctx, currentEditions+`
		UPDATE edition_items SET read_at = ?
		 WHERE item_id = ?
		   AND edition_id IN (SELECT id FROM current WHERE principal_id = ?)`,
		nullTime(readAt(now, read)), itemID, principalID)
	if err != nil {
		return err
	}
	// At least one, rather than exactly one: an article on two pages updates two rows, and
	// that is the point of the query above.
	if err := expectSome(res, NotFound("that article is not on any of your pages")); err != nil {
		return err
	}

	if read {
		// INSERT ... SELECT so the article's details are copied inside the transaction
		// rather than read out and written back. OR REPLACE because reading something,
		// unreading it and reading it again should record the latest moment, not fail.
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO read_articles
			   (principal_id, item_id, feed_id, title, link, published_at, read_at)
			 SELECT ?, i.id, i.feed_id, i.title, i.link, i.published_at, ?
			   FROM items i WHERE i.id = ?`,
			principalID, unix(now), itemID); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx,
		`DELETE FROM read_articles WHERE principal_id = ? AND item_id = ?`,
		principalID, itemID); err != nil {
		return err
	}

	return tx.Commit()
}

// ReadArticles is what this person has read lately, newest first.
//
// Bounded by [ReadListLimit] rather than by age. The record outlives the list on purpose: what
// somebody has read is what keeps an article they have finished with off their pages, and that
// has to hold for as long as they follow the feed.
func (s *Store) ReadArticles(ctx context.Context, principalID string) ([]*ReadArticle, error) {
	rows, err := s.derived.QueryContext(ctx,
		`SELECT item_id, feed_id, title, link, published_at, read_at
		   FROM read_articles
		  WHERE principal_id = ?
		  ORDER BY read_at DESC
		  LIMIT ?`,
		principalID, ReadListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ReadArticle
	for rows.Next() {
		var (
			a         ReadArticle
			published int64
			read      int64
		)
		if err := rows.Scan(&a.ItemID, &a.FeedID, &a.Title, &a.Link, &published, &read); err != nil {
			return nil, err
		}
		a.PublishedAt = time.Unix(published, 0).UTC()
		a.ReadAt = time.Unix(read, 0).UTC()
		out = append(out, &a)
	}
	return out, rows.Err()
}

// PruneReadArticles drops what was read on feeds that no longer exist.
//
// Nothing here goes by age. Unfollowing a feed takes what was read there with it, and that
// happens the moment somebody unfollows — see DeleteSubscription. This is the safety net for
// the two ways that can be missed: the delete is across two databases and so cannot be in one
// transaction with the unsubscribe, and a feed the last follower drops is collected wholesale
// by the sweep rather than one subscription at a time.
func (s *Store) PruneReadArticles(ctx context.Context, liveFeedIDs []string) (int64, error) {
	if len(liveFeedIDs) == 0 {
		res, err := s.derived.ExecContext(ctx, `DELETE FROM read_articles`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}

	args, placeholders := inList(liveFeedIDs)
	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM read_articles WHERE feed_id NOT IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ForgetReadArticles drops what one person read on one feed.
//
// Called when they unfollow it. The record's job is to keep an article they have finished with
// off their pages, and a feed they no longer follow has no pages to be kept off.
func (s *Store) ForgetReadArticles(ctx context.Context, principalID, feedID string) (int64, error) {
	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM read_articles WHERE principal_id = ? AND feed_id = ?`, principalID, feedID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func readAt(now time.Time, read bool) time.Time {
	if read {
		return now
	}
	return time.Time{}
}

// ReleaseUnread returns the live page's unread articles to the pool, and reports how many.
//
// A scheduled page turn is time passing: you had your day with that page and it is gone,
// articles and all. A manual regeneration is not that — nothing has elapsed, somebody has
// just asked for a different page — so burning articles they never looked at would be
// charging them for a day that did not happen.
//
// In practice this is what makes the button usable at all while setting an instance up.
// Without it the first press consumes every article the feeds have published, and the
// second answers "nothing new has been published" — which is true, and useless, at exactly
// the moment somebody is tuning priorities and wants to see what changed.
//
// Read articles are deliberately not released. Those are dealt with either way.
//
// The hashes cannot be matched in SQL: `shown` stores a digest of the guid and SQLite has
// no sha256, so there is nothing to join on. The live page is at most `edition_size` rows,
// so reading them and deleting by computed hash is a couple of hundred statements at the
// very worst.
func (s *Store) ReleaseUnread(ctx context.Context, pageID string) (int64, error) {
	// This page's unread articles, and only this page's record of having shown them.
	//
	// Each page keeps its own memory, so releasing here says nothing about anywhere else: an
	// article still sitting on another page stays shown there, because that page's row is a
	// different row.
	rows, err := s.derived.QueryContext(ctx, currentEditions+`
		SELECT i.feed_id, i.guid
		  FROM current ed
		  JOIN edition_items e ON e.edition_id = ed.id
		  JOIN items i         ON i.id = e.item_id
		 WHERE ed.page_id = ?
		   AND e.read_at IS NULL`,
		pageID)
	if err != nil {
		return 0, err
	}

	type article struct{ feedID, guid string }
	var unread []article
	for rows.Next() {
		var a article
		if err := rows.Scan(&a.feedID, &a.guid); err != nil {
			rows.Close()
			return 0, err
		}
		unread = append(unread, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(unread) == 0 {
		return 0, nil
	}

	tx, err := s.derived.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`DELETE FROM shown WHERE page_id = ? AND feed_id = ? AND guid_hash = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var released int64
	for _, a := range unread {
		res, err := stmt.ExecContext(ctx, pageID, a.feedID, GUIDHash(a.guid))
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		released += n
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return released, nil
}

// DeleteEditionsExcept collects the editions of pages that no longer exist.
//
// Two lists, because two different things are being collected. Editions belong to pages, and a
// page can be deleted while its owner stays; shown and read_articles belong to a person and
// outlive any one of their pages. Both tables live in the other database from the rows that
// would be their foreign keys, so the lists have to be passed in and the sweep runs this
// periodically.
func (s *Store) DeleteEditionsExcept(ctx context.Context, livePageIDs, livePrincipalIDs []string) (int64, error) {
	if len(livePageIDs) == 0 || len(livePrincipalIDs) == 0 {
		res, err := s.derived.ExecContext(ctx, `DELETE FROM editions`)
		if err != nil {
			return 0, err
		}
		for _, table := range []string{"shown", "read_articles"} {
			if _, err := s.derived.ExecContext(ctx, `DELETE FROM `+table); err != nil {
				return 0, err
			}
		}
		return res.RowsAffected()
	}

	pageArgs, pagePlaceholders := inList(livePageIDs)
	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM editions WHERE page_id NOT IN (`+pagePlaceholders+`)`, pageArgs...)
	if err != nil {
		return 0, err
	}
	removed, _ := res.RowsAffected()

	// shown belongs to a page and read_articles to a person, so they are collected against
	// different lists. A page removed on its own should lose what it had shown — that page is
	// gone — while what somebody read outlives any page they read it on.
	if _, err := s.derived.ExecContext(ctx,
		`DELETE FROM shown WHERE page_id NOT IN (`+pagePlaceholders+`)`, pageArgs...); err != nil {
		return removed, err
	}

	args, placeholders := inList(livePrincipalIDs)
	if _, err := s.derived.ExecContext(ctx,
		`DELETE FROM read_articles WHERE principal_id NOT IN (`+placeholders+`)`, args...); err != nil {
		return removed, err
	}
	return removed, nil
}

// PruneOldEditions removes every edition a page has moved on from.
//
// Editions accumulate because nothing deletes one on the way in — see [AddEdition] — so this is
// the other half of that: the newest edition of each page is the page, and the ones behind it
// are history nobody reads. Their items and read marks go with them by cascade, and the durable
// record of what somebody read is read_articles, which this does not touch.
func (s *Store) PruneOldEditions(ctx context.Context) (int64, error) {
	res, err := s.derived.ExecContext(ctx, currentEditions+`
		DELETE FROM editions WHERE id NOT IN (SELECT id FROM current)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DropEditions removes everything one page has composed.
//
// For a page whose filter has changed to something nothing matches. Its edition was chosen
// under the old filter and may hold nothing the new one would have picked, so leaving it up
// would be showing somebody a page they have just told the program not to want. Empty is the
// honest answer, and it is the same empty a page has before its first composition.
func (s *Store) DropEditions(ctx context.Context, pageID string) error {
	_, err := s.derived.ExecContext(ctx, `DELETE FROM editions WHERE page_id = ?`, pageID)
	return err
}

// MarkFeedRead marks everything one person can see from one feed as read.
//
// `before` bounds it by when an article was published — everything older than a day, a week, a
// month. The zero time means all of it.
//
// Both halves of "read" are written, and they are not the same thing. The record in
// read_articles is what keeps an article off a page it has not reached yet: Candidates skips
// what this person has read, so marking a feed's backlog read means its old articles are never
// offered, which is the whole point of doing this when following a feed again after a while.
// The marks on live editions are what greys the cards already on screen.
//
// Articles already read are left alone rather than re-stamped. "Mark everything read" is about
// the things that are not, and moving an article somebody finished with last week to the top of
// Recently read would be answering a different question.
func (s *Store) MarkFeedRead(ctx context.Context, principalID, feedID string, before time.Time) (int64, error) {
	now := s.Now()

	// Zero means no bound. Written as a flag rather than a far-future date, because a date
	// this far out is a magic number somebody has to recognise.
	bounded := 0
	cutoff := int64(0)
	if !before.IsZero() {
		bounded, cutoff = 1, unix(before)
	}

	tx, err := s.derived.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO read_articles
		    (principal_id, item_id, feed_id, title, link, published_at, read_at)
		SELECT ?, i.id, i.feed_id, i.title, i.link, i.published_at, ?
		  FROM items i
		 WHERE i.feed_id = ? AND (? = 0 OR i.published_at < ?)`,
		principalID, unix(now), feedID, bounded, cutoff)
	if err != nil {
		return 0, err
	}
	marked, _ := res.RowsAffected()

	// And the cards on screen. Only the ones not already read, for the same reason: a mark
	// somebody made themselves is theirs, and this should not restamp it.
	if _, err := tx.ExecContext(ctx, currentEditions+`
		UPDATE edition_items SET read_at = ?
		 WHERE read_at IS NULL
		   AND edition_id IN (SELECT id FROM current WHERE principal_id = ?)
		   AND item_id IN (
		         SELECT id FROM items
		          WHERE feed_id = ? AND (? = 0 OR published_at < ?))`,
		unix(now), principalID, feedID, bounded, cutoff); err != nil {
		return 0, err
	}

	return marked, tx.Commit()
}
