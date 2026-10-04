package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"bystander/internal/ids"
)

// SavedPageName is what the page of saved articles is called when the first save makes it.
// It is a page like any other from then on, and can be renamed.
const SavedPageName = "Read later"

// savedPageSlug is where that page lives, with a number after it if the person already has a
// page there.
const savedPageSlug = "later"

// SavedArticle is something somebody kept to read later, as they kept it: a copy, source and all,
// because the article and its feed both go before a save does. See docs/entities.md.
type SavedArticle struct {
	Item
	SourceTitle string
	SourceURL   string
	SavedAt     time.Time
	// ReadAt is when it was read on the page of saved articles, zero until then. Its own mark,
	// apart from read_articles — see SetSavedRead.
	ReadAt time.Time
}

// SaveArticle keeps an article for later, marks it read, and makes the page that shows such
// things if this is the person's first.
//
// Any article that still exists, including one on somebody else's published page: saving it is
// a fact about the person doing it, as reading it is. Saving it twice keeps the first time.
//
// Read, because putting something aside is dealing with it on the page it was found on. That is
// the ordinary read mark, which the saved page does not look at — see SetSavedRead — so the
// article arrives there unread.
//
// And the saved page's edition is dropped, so the next look at it composes one that has the new
// save on it rather than waiting for its turn. Its clock is left alone.
func (s *Store) SaveArticle(ctx context.Context, principalID, itemID string) error {
	item, err := s.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	feeds, err := s.FeedsByIDs(ctx, []string{item.FeedID})
	if err != nil {
		return err
	}
	var title, site string
	if feed := feeds[item.FeedID]; feed != nil {
		title, site = feed.Title, feed.SiteURL
	}

	now := s.Now()
	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO saved
		   (principal_id, item_id, feed_id, guid, title, link, author, summary, image_url,
		    image_width, image_height, published_at, source_title, source_url, saved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		principalID, item.ID, item.FeedID, item.GUID, item.Title, item.Link, item.Author,
		item.Summary, item.ImageURL, item.ImageWidth, item.ImageHeight, unix(item.PublishedAt),
		title, site, unix(now))
	if err != nil {
		return fmt.Errorf("save article %s: %w", itemID, err)
	}
	// Only a save that happened recomposes the saved page; a second PUT is not a new save.
	inserted, _ := res.RowsAffected()

	pageID, err := ensureSavedPage(ctx, tx, principalID, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if inserted == 0 {
		return nil
	}
	// After the commit, and in the other database: a failure here leaves an article saved and
	// not yet on its page, which is the harmless way round.
	if err := s.SetRead(ctx, principalID, itemID, true); err != nil {
		return err
	}
	return s.DropEditions(ctx, pageID)
}

// SetSavedRead marks a saved article read or unread on the page of saved articles.
//
// A mark of its own, on the save, and the ordinary one in read_articles is left alone — as that
// one leaves this alone. The two pages are read for different reasons: saving reads an article
// where it was found, which must not grey it on arrival where it was saved to, and reading it
// there is not reading it again on the Front Page. One mark between them had each undoing the
// other. Something not saved has nothing to mark, which is not an error, as with SetRead.
func (s *Store) SetSavedRead(ctx context.Context, principalID, itemID string, read bool) error {
	_, err := s.main.ExecContext(ctx,
		`UPDATE saved SET read_at = ? WHERE principal_id = ? AND item_id = ?`,
		nullTime(readAt(s.Now(), read)), principalID, itemID)
	return err
}

// ensureSavedPage makes the page of saved articles if this person has none.
//
// Inside the save's transaction, so a save never leaves an article kept with nowhere to show it.
// Due at once, like any new page, so the first save is on a page by the next tick rather than
// tomorrow. Not counted against MaxPages: nobody asked for it by name, and refusing a save
// because somebody already has twenty pages would be refusing the wrong thing.
//
// Returns the page's id, made or found.
func ensureSavedPage(ctx context.Context, tx *sql.Tx, principalID string, now time.Time) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM pages WHERE principal_id = ? AND is_saved = 1`, principalID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	slug := savedPageSlug
	for n := 2; ; n++ {
		var taken bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pages WHERE principal_id = ? AND slug = ?)`,
			principalID, slug).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			break
		}
		slug = fmt.Sprintf("%s-%d", savedPageSlug, n)
	}

	id = ids.New(ids.Page)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO pages (id, principal_id, name, slug, is_main, is_saved,
		                    edition_interval, edition_size, next_edition_at, created_at)
		 VALUES (?, ?, ?, ?, 0, 1, ?, ?, ?, ?)`,
		id, principalID, SavedPageName, slug,
		int64((24 * time.Hour).Seconds()), 60, unix(now), unix(now))
	if err != nil {
		return "", fmt.Errorf("make the page of saved articles: %w", err)
	}
	return id, nil
}

// UnsaveArticle lets go of something kept for later. Letting go of something never kept is not
// an error, for the same reason unmarking something unread is not.
//
// The page is left alone, here and when the last article goes: a page with nothing saved on it
// says so, and somebody who saves again finds it where they left it.
func (s *Store) UnsaveArticle(ctx context.Context, principalID, itemID string) error {
	_, err := s.main.ExecContext(ctx,
		`DELETE FROM saved WHERE principal_id = ? AND item_id = ?`, principalID, itemID)
	return err
}

// SavedAmong is which of these articles this person has saved, with what they kept of each.
//
// For a page being read: whether each card is saved, and the source of any card whose feed has
// been collected. An empty principal — a stranger — has saved nothing.
func (s *Store) SavedAmong(ctx context.Context, principalID string, itemIDs []string) (map[string]*SavedArticle, error) {
	out := make(map[string]*SavedArticle)
	if principalID == "" || len(itemIDs) == 0 {
		return out, nil
	}
	args, marks := inList(itemIDs)
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+savedColumns+` FROM saved
		  WHERE principal_id = ? AND item_id IN (`+marks+`)`,
		append([]any{principalID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanSaved(rows)
		if err != nil {
			return nil, err
		}
		out[a.ID] = a
	}
	return out, rows.Err()
}

// SavedQueues is what the page of saved articles can draw from: one queue per article, each in
// the band it belongs in. One per article, not one for the lot — see "The page of saved
// articles" in docs/edition.md.
//
// The live article is used where it still exists, since its picture may have been measured since
// it was saved. Where it has been pruned the saved copy stands in, and AddEdition puts it back.
func (s *Store) SavedQueues(ctx context.Context, pageID, principalID string) (map[string]*Queue, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+savedColumns+` FROM saved WHERE principal_id = ? ORDER BY saved_at DESC`,
		principalID)
	if err != nil {
		return nil, err
	}
	var saved []*SavedArticle
	for rows.Next() {
		a, err := scanSaved(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		saved = append(saved, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(saved) == 0 {
		return map[string]*Queue{}, nil
	}

	itemIDs := make([]string, len(saved))
	for i, a := range saved {
		itemIDs[i] = a.ID
	}
	args, marks := inList(itemIDs)

	live := make(map[string]*Item, len(saved))
	itemRows, err := s.derived.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, err
	}
	for itemRows.Next() {
		item, err := scanItem(itemRows)
		if err != nil {
			itemRows.Close()
			return nil, err
		}
		live[item.ID] = item
	}
	itemRows.Close()
	if err := itemRows.Err(); err != nil {
		return nil, err
	}

	shown := make(map[string]bool)
	shownRows, err := s.derived.QueryContext(ctx,
		`SELECT feed_id, guid_hash FROM shown WHERE page_id = ?`, pageID)
	if err != nil {
		return nil, err
	}
	for shownRows.Next() {
		var feedID string
		var hash []byte
		if err := shownRows.Scan(&feedID, &hash); err != nil {
			shownRows.Close()
			return nil, err
		}
		shown[feedID+"\x00"+string(hash)] = true
	}
	shownRows.Close()
	if err := shownRows.Err(); err != nil {
		return nil, err
	}

	now := s.Now()
	out := make(map[string]*Queue, len(saved))
	for _, a := range saved {
		item := live[a.ID]
		if item == nil {
			copied := a.Item
			copied.FetchedAt = now
			item = &copied
		}
		q := &Queue{}
		switch {
		case !a.ReadAt.IsZero():
			q.Read = []*Item{item}
		case shown[item.FeedID+"\x00"+string(GUIDHash(item.GUID))]:
			q.Unread = []*Item{item}
		default:
			q.Fresh = []*Item{item}
		}
		out[item.ID] = q
	}
	return out, nil
}

const savedColumns = `item_id, feed_id, guid, title, link, author, summary, image_url, image_width, image_height, published_at, source_title, source_url, saved_at, read_at`

func scanSaved(row interface{ Scan(...any) error }) (*SavedArticle, error) {
	var (
		a         SavedArticle
		published int64
		saved     int64
		read      sql.NullInt64
	)
	if err := row.Scan(&a.ID, &a.FeedID, &a.GUID, &a.Title, &a.Link, &a.Author, &a.Summary,
		&a.ImageURL, &a.ImageWidth, &a.ImageHeight, &published, &a.SourceTitle, &a.SourceURL,
		&saved, &read); err != nil {
		return nil, err
	}
	a.PublishedAt = time.Unix(published, 0).UTC()
	a.SavedAt = time.Unix(saved, 0).UTC()
	a.ReadAt = timeFrom(read)
	return &a, nil
}
