package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"bystander/internal/ids"
)

// Slot is how prominently an article is laid out.
//
// Decided at generation time and stored, so the page is identical on every reload until
// the next edition replaces it. The type lives here because the schema's CHECK constraint
// holds the same four values.
type Slot string

const (
	SlotLead     Slot = "lead"
	SlotFeature  Slot = "feature"
	SlotStandard Slot = "standard"
	SlotBrief    Slot = "brief"
)

// Edition is one person's live front page.
type Edition struct {
	ID          string
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

// ReplaceEdition swaps a principal's page for a new one.
//
// One transaction, in this order: delete the old edition, insert the new one and its
// items, record what was shown. Delete-before-insert is forced by the unique index on
// editions.principal_id — exactly one live edition per person — and is safe because it is
// one transaction: the previous edition stays visible to readers until this commits, so a
// crash half way through leaves somebody with yesterday's page rather than with nothing.
//
// Discarding the old edition takes its read marks with it, by cascade. That is the whole
// design: a mark belongs to the edition it was made in.
func (s *Store) ReplaceEdition(ctx context.Context, principalID string, seed int64, size int, picks []Pick) (*Edition, error) {
	now := s.Now()
	ed := &Edition{
		ID:          ids.New(ids.Edition),
		PrincipalID: principalID,
		GeneratedAt: now,
		Seed:        seed,
		Size:        size,
	}

	tx, err := s.derived.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM editions WHERE principal_id = ?`, principalID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO editions (id, principal_id, generated_at, seed, size) VALUES (?, ?, ?, ?, ?)`,
		ed.ID, principalID, unix(now), seed, size); err != nil {
		return nil, fmt.Errorf("create edition: %w", err)
	}

	item, err := tx.PrepareContext(ctx,
		`INSERT INTO edition_items (edition_id, item_id, rank, slot) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer item.Close()

	// INSERT OR REPLACE rather than INSERT: an article can legitimately be shown again
	// after its previous record was pruned, and re-stamping the date is exactly right.
	shown, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO shown (principal_id, feed_id, guid_hash, shown_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer shown.Close()

	for _, pick := range picks {
		if _, err := item.ExecContext(ctx, ed.ID, pick.Item.ID, pick.Rank, string(pick.Slot)); err != nil {
			return nil, fmt.Errorf("place article %s: %w", pick.Item.ID, err)
		}
		if _, err := shown.ExecContext(ctx, principalID, pick.Item.FeedID, GUIDHash(pick.Item.GUID), unix(now)); err != nil {
			return nil, fmt.Errorf("record article %s as shown: %w", pick.Item.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ed, nil
}

// CurrentEdition returns a principal's live page, or ErrNotFound before their first
// generation.
func (s *Store) CurrentEdition(ctx context.Context, principalID string) (*Edition, []*EditionItem, error) {
	var (
		ed        Edition
		generated int64
	)
	err := s.derived.QueryRowContext(ctx,
		`SELECT id, principal_id, generated_at, seed, size FROM editions WHERE principal_id = ?`,
		principalID).Scan(&ed.ID, &ed.PrincipalID, &generated, &ed.Seed, &ed.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, NotFound("no page has been generated yet")
	}
	if err != nil {
		return nil, nil, err
	}
	ed.GeneratedAt = time.Unix(generated, 0).UTC()

	rows, err := s.derived.QueryContext(ctx,
		`SELECT i.id, i.feed_id, i.guid, i.title, i.link, i.author, i.summary, i.image_url,
		        i.published_at, i.fetched_at, e.rank, e.slot, e.read_at
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
			item      Item
			published int64
			fetched   int64
			slot      string
			read      sql.NullInt64
			entry     EditionItem
		)
		if err := rows.Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link, &item.Author,
			&item.Summary, &item.ImageURL, &published, &fetched, &entry.Rank, &slot, &read); err != nil {
			return nil, nil, err
		}
		item.PublishedAt = time.Unix(published, 0).UTC()
		item.FetchedAt = time.Unix(fetched, 0).UTC()
		entry.Item = &item
		entry.Slot = Slot(slot)
		entry.ReadAt = timeFrom(read)
		out = append(out, &entry)
	}
	return &ed, out, rows.Err()
}

// SetRead marks an article read or unread on this principal's live page.
//
// Scoped through the editions table rather than trusting the item id: an item id is
// shared by everybody who follows that feed, so without the join one person could mark
// another's page.
func (s *Store) SetRead(ctx context.Context, principalID, itemID string, read bool) error {
	res, err := s.derived.ExecContext(ctx,
		`UPDATE edition_items SET read_at = ?
		  WHERE item_id = ?
		    AND edition_id = (SELECT id FROM editions WHERE principal_id = ?)`,
		nullTime(readAt(s.Now(), read)), itemID, principalID)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("that article is not on your page"))
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
func (s *Store) ReleaseUnread(ctx context.Context, principalID string) (int64, error) {
	rows, err := s.derived.QueryContext(ctx,
		`SELECT i.feed_id, i.guid
		   FROM edition_items e
		   JOIN items i ON i.id = e.item_id
		  WHERE e.read_at IS NULL
		    AND e.edition_id = (SELECT id FROM editions WHERE principal_id = ?)`,
		principalID)
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
		`DELETE FROM shown WHERE principal_id = ? AND feed_id = ? AND guid_hash = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var released int64
	for _, a := range unread {
		res, err := stmt.ExecContext(ctx, principalID, a.feedID, GUIDHash(a.guid))
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

// DeleteEditionsExcept collects the pages of accounts that no longer exist.
//
// Principals live in the other database, so this cannot be a foreign key: the list of who
// is still here has to be passed in, and the sweep runs it periodically.
func (s *Store) DeleteEditionsExcept(ctx context.Context, livePrincipalIDs []string) (int64, error) {
	if len(livePrincipalIDs) == 0 {
		res, err := s.derived.ExecContext(ctx, `DELETE FROM editions`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}

	args := make([]any, 0, len(livePrincipalIDs))
	for _, id := range livePrincipalIDs {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(livePrincipalIDs)), ",")

	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM editions WHERE principal_id NOT IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	removed, _ := res.RowsAffected()

	if _, err := s.derived.ExecContext(ctx,
		`DELETE FROM shown WHERE principal_id NOT IN (`+placeholders+`)`, args...); err != nil {
		return removed, err
	}
	return removed, nil
}
