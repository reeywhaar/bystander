package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ExportBatch is how many rows one page of an export reads at a time.
//
// The archive is written straight to the socket, and the two long sections are read in
// batches rather than from one open cursor. That is not about memory — it is about the
// single connection: `SetMaxOpenConns(1)` means a cursor held open while the response
// blocks on a slow client would stall every other request against that database for as long
// as the download takes. A batch is read, the connection is released, and the bytes go out
// with nothing held.
//
// Five hundred rows of an article's title and link is a few hundred kilobytes, which is a
// negligible amount to hold and few enough round trips that the query cost disappears.
const ExportBatch = 500

// ExportedAccount is who the export belongs to.
//
// No password hash, no session, no invitation. This is a copy of somebody's own data for
// them to keep, not a backup of the row — and a bcrypt hash in a file on a laptop is a
// liability to whoever holds it and of no use to them whatsoever.
type ExportedAccount struct {
	Username      string `json:"username"`
	Role          string `json:"role"`
	CreatedAt     int64  `json:"created_at"`
	PublicName    string `json:"public_name"`
	RecoveryEmail string `json:"recovery_email"`
}

// ExportedTag is one tag, named by its path rather than by its id.
//
// `parent` is the parent's name, not its id: an id means nothing outside the instance that
// minted it, and the point of an export is to be read somewhere else.
type ExportedTag struct {
	Name      string `json:"name"`
	Parent    string `json:"parent"`
	Priority  int    `json:"priority"`
	CreatedAt int64  `json:"created_at"`
}

// ExportedFeed is a subscription and the feed under it, flattened.
//
// The split between what a person chose and what the fetcher learned is a fact about this
// schema and not about their data, so it is not reproduced here: `url`, `title` and `tags`
// are what another reader could import, and `priority` and `max_article_age` are what this
// one would want back.
type ExportedFeed struct {
	URL           string   `json:"url"`
	CanonicalURL  string   `json:"canonical_url"`
	Title         string   `json:"title"`
	SiteURL       string   `json:"site_url"`
	Priority      int      `json:"priority"`
	MaxArticleAge int64    `json:"max_article_age"`
	Tags          []string `json:"tags"`
	CreatedAt     int64    `json:"created_at"`
}

// ExportedPage is one front page and what it draws from.
type ExportedPage struct {
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	IsMain          bool     `json:"is_main"`
	EditionInterval int64    `json:"edition_interval"`
	EditionSize     int      `json:"edition_size"`
	MaxArticleAge   int64    `json:"max_article_age"`
	IncludeTags     []string `json:"include_tags"`
	ExcludeTags     []string `json:"exclude_tags"`
	IncludeFeeds    []string `json:"include_feeds"`
	ExcludeFeeds    []string `json:"exclude_feeds"`
	Published       bool     `json:"published"`
	PublishSlug     string   `json:"publish_slug"`
	Indexable       bool     `json:"indexable"`
	CreatedAt       int64    `json:"created_at"`
}

// ExportedArticle is one article, read or unread.
//
// One shape for both, because they differ by one field and two shapes would mean anybody
// reading the file had to learn two. `read_at` is zero on an unread one.
type ExportedArticle struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	FeedURL     string `json:"feed_url"`
	FeedTitle   string `json:"feed_title"`
	PublishedAt int64  `json:"published_at"`
	ReadAt      int64  `json:"read_at,omitempty"`

	// FeedID and ID never reach the file. The first is what FeedURL and FeedTitle are
	// looked up from, across two databases that cannot be joined; the second is half the
	// keyset cursor. Both are ids, which mean nothing outside the instance that minted
	// them, and an export is a thing to be read somewhere else.
	FeedID string `json:"-"`
	ID     string `json:"-"`
}

// ExportAccount is the account itself, without anything secret about it.
func (s *Store) ExportAccount(ctx context.Context, principalID string) (*ExportedAccount, error) {
	p, err := s.PrincipalByID(ctx, principalID)
	if err != nil {
		return nil, err
	}
	email, err := s.RecoveryEmail(ctx, principalID)
	if err != nil {
		return nil, err
	}
	return &ExportedAccount{
		Username:      p.Username,
		Role:          string(p.Role),
		CreatedAt:     p.CreatedAt.Unix(),
		PublicName:    p.Slug,
		RecoveryEmail: email,
	}, nil
}

// ExportTags is somebody's whole taxonomy, parents named rather than referenced.
func (s *Store) ExportTags(ctx context.Context, principalID string) ([]ExportedTag, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT child.name, ifnull(parent.name, ''), child.priority, child.created_at
		   FROM tags child
		   LEFT JOIN tags parent ON parent.id = child.parent_id
		  WHERE child.principal_id = ?
		  ORDER BY ifnull(parent.name, ''), child.name`,
		principalID)
	if err != nil {
		return nil, fmt.Errorf("export tags: %w", err)
	}
	defer rows.Close()

	out := []ExportedTag{}
	for rows.Next() {
		var tag ExportedTag
		if err := rows.Scan(&tag.Name, &tag.Parent, &tag.Priority, &tag.CreatedAt); err != nil {
			return nil, fmt.Errorf("export tags: %w", err)
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// ExportFeeds is everything somebody follows, with the tags they filed each under.
//
// Bounded without a limit for the same reason the API is not paginated: nobody has thousands
// of feeds, and the day somebody does this is not the query that breaks.
func (s *Store) ExportFeeds(ctx context.Context, principalID string) ([]ExportedFeed, error) {
	tags, err := s.subscriptionTagNames(ctx, principalID)
	if err != nil {
		return nil, err
	}

	rows, err := s.main.QueryContext(ctx,
		`SELECT s.id, f.url, f.canonical_url,
		        CASE WHEN s.title_override <> '' THEN s.title_override ELSE f.title END,
		        f.site_url, s.priority, s.max_article_age, s.created_at
		   FROM subscriptions s
		   JOIN feeds f ON f.id = s.feed_id
		  WHERE s.principal_id = ?
		  ORDER BY f.title, f.canonical_url`,
		principalID)
	if err != nil {
		return nil, fmt.Errorf("export feeds: %w", err)
	}
	defer rows.Close()

	out := []ExportedFeed{}
	for rows.Next() {
		var (
			id   string
			feed ExportedFeed
		)
		if err := rows.Scan(&id, &feed.URL, &feed.CanonicalURL, &feed.Title, &feed.SiteURL,
			&feed.Priority, &feed.MaxArticleAge, &feed.CreatedAt); err != nil {
			return nil, fmt.Errorf("export feeds: %w", err)
		}
		feed.Tags = tags[id]
		if feed.Tags == nil {
			// An empty array rather than null. A field that is sometimes a list and
			// sometimes nothing is a field every reader of the file has to special-case.
			feed.Tags = []string{}
		}
		out = append(out, feed)
	}
	return out, rows.Err()
}

// subscriptionTagNames maps each subscription to the names it is filed under.
func (s *Store) subscriptionTagNames(ctx context.Context, principalID string) (map[string][]string, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT st.subscription_id, t.name
		   FROM subscription_tags st
		   JOIN tags t ON t.id = st.tag_id
		   JOIN subscriptions s ON s.id = st.subscription_id
		  WHERE s.principal_id = ?
		  ORDER BY t.name`,
		principalID)
	if err != nil {
		return nil, fmt.Errorf("export feed tags: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("export feed tags: %w", err)
		}
		out[id] = append(out[id], name)
	}
	return out, rows.Err()
}

// ExportPages is somebody's front pages and what each draws from.
func (s *Store) ExportPages(ctx context.Context, principalID string) ([]ExportedPage, error) {
	tags, err := s.pageTagNames(ctx, principalID)
	if err != nil {
		return nil, err
	}
	feeds, err := s.pageFeedURLs(ctx, principalID)
	if err != nil {
		return nil, err
	}

	rows, err := s.main.QueryContext(ctx,
		`SELECT id, name, slug, is_main, edition_interval, edition_size, max_article_age,
		        published, publish_slug, indexable, created_at
		   FROM pages WHERE principal_id = ?
		  ORDER BY is_main DESC, created_at`,
		principalID)
	if err != nil {
		return nil, fmt.Errorf("export pages: %w", err)
	}
	defer rows.Close()

	out := []ExportedPage{}
	for rows.Next() {
		var (
			id                          string
			isMain, published, indexing int
			page                        ExportedPage
		)
		if err := rows.Scan(&id, &page.Name, &page.Slug, &isMain, &page.EditionInterval,
			&page.EditionSize, &page.MaxArticleAge, &published, &page.PublishSlug,
			&indexing, &page.CreatedAt); err != nil {
			return nil, fmt.Errorf("export pages: %w", err)
		}
		page.IsMain = isMain == 1
		page.Published = published == 1
		page.Indexable = indexing == 1
		page.IncludeTags = orEmpty(tags[filterKey{id, "include"}])
		page.ExcludeTags = orEmpty(tags[filterKey{id, "exclude"}])
		page.IncludeFeeds = orEmpty(feeds[filterKey{id, "include"}])
		page.ExcludeFeeds = orEmpty(feeds[filterKey{id, "exclude"}])
		out = append(out, page)
	}
	return out, rows.Err()
}

// filterKey is a page and which side of its filter a row is on.
type filterKey struct{ pageID, mode string }

func (s *Store) pageTagNames(ctx context.Context, principalID string) (map[filterKey][]string, error) {
	return s.pageFilter(ctx,
		`SELECT pt.page_id, pt.mode, t.name
		   FROM page_tags pt
		   JOIN tags t ON t.id = pt.tag_id
		   JOIN pages p ON p.id = pt.page_id
		  WHERE p.principal_id = ?
		  ORDER BY t.name`, principalID, "export page tags")
}

// pageFeedURLs names a page's per-feed overrides by URL, for the same reason tags are named
// rather than referenced: an id is meaningless outside the instance that minted it.
func (s *Store) pageFeedURLs(ctx context.Context, principalID string) (map[filterKey][]string, error) {
	return s.pageFilter(ctx,
		`SELECT pf.page_id, pf.mode, f.canonical_url
		   FROM page_feeds pf
		   JOIN feeds f ON f.id = pf.feed_id
		   JOIN pages p ON p.id = pf.page_id
		  WHERE p.principal_id = ?
		  ORDER BY f.canonical_url`, principalID, "export page feeds")
}

func (s *Store) pageFilter(ctx context.Context, query, principalID, what string) (map[filterKey][]string, error) {
	rows, err := s.main.QueryContext(ctx, query, principalID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()

	out := map[filterKey][]string{}
	for rows.Next() {
		var key filterKey
		var name string
		if err := rows.Scan(&key.pageID, &key.mode, &name); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out[key] = append(out[key], name)
	}
	return out, rows.Err()
}

// FeedNames maps a feed id to what it is called and where it lives, for the sections that
// have to name a feed across the two databases.
//
// The article tables live in derived.db and feeds live in main.db, and the two are never
// ATTACHed to each other — so an article's feed cannot be named in SQL and is named in Go
// instead. Bounded by how many feeds somebody follows, which is not a large number.
func (s *Store) FeedNames(ctx context.Context, principalID string) (map[string][2]string, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT f.id, f.canonical_url,
		        CASE WHEN s.title_override <> '' THEN s.title_override ELSE f.title END
		   FROM subscriptions s JOIN feeds f ON f.id = s.feed_id
		  WHERE s.principal_id = ?`,
		principalID)
	if err != nil {
		return nil, fmt.Errorf("export feed names: %w", err)
	}
	defer rows.Close()

	out := map[string][2]string{}
	for rows.Next() {
		var id, url, title string
		if err := rows.Scan(&id, &url, &title); err != nil {
			return nil, fmt.Errorf("export feed names: %w", err)
		}
		out[id] = [2]string{url, title}
	}
	return out, rows.Err()
}

// ExportCursor is where a batch resumes: the sort key and the id of the last row of the
// previous batch. Nil starts at the beginning.
//
// A type rather than a zero value meaning "from the start". A timestamp of zero should not
// be spelled the same way as "no cursor" — a single row with a zero sort key would then
// restart the scan on every batch and the archive would never end.
type ExportCursor struct {
	At int64
	ID string
}

// After returns where the batch that ends with this article resumes.
func (a ExportedArticle) After(at int64) *ExportCursor {
	return &ExportCursor{At: at, ID: a.ID}
}

// ExportRead is one page of what somebody has read, most recent first.
//
// Keyset rather than OFFSET: the two long sections are read in batches while the archive is
// being written, and an OFFSET scan re-walks everything before it on every batch — quadratic
// over a reader with a year of history, which is exactly who this is for. The index
// `read_articles_when(principal_id, read_at DESC)` is what makes each batch a seek.
func (s *Store) ExportRead(ctx context.Context, principalID string, after *ExportCursor, limit int) ([]ExportedArticle, error) {
	where, args := "", []any{principalID}
	if after != nil {
		where = " AND (read_at, item_id) < (?, ?)"
		args = append(args, after.At, after.ID)
	}
	args = append(args, limit)

	rows, err := s.derived.QueryContext(ctx,
		`SELECT feed_id, title, link, published_at, read_at, item_id
		   FROM read_articles
		  WHERE principal_id = ?`+where+`
		  ORDER BY read_at DESC, item_id DESC
		  LIMIT ?`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("export read articles: %w", err)
	}
	defer rows.Close()

	return scanExported(rows, "export read articles")
}

// ExportUnread is one page of what is waiting, newest first.
//
// "Unread" is not a stored fact — it is every article held for a feed somebody follows that
// they have not read — so this is bounded by how long items are kept, which is thirty days.
// The export says so about itself rather than leaving somebody to conclude their reader lost
// the rest.
//
// The feed ids come from main.db and the items from derived.db, which are never ATTACHed, so
// the set of followed feeds is passed in rather than joined.
func (s *Store) ExportUnread(ctx context.Context, principalID string, feedIDs []string, after *ExportCursor, limit int) ([]ExportedArticle, error) {
	if len(feedIDs) == 0 {
		return nil, nil
	}
	args, marks := inList(feedIDs)
	args = append(args, principalID)
	where := ""
	if after != nil {
		where = " AND (i.published_at, i.id) < (?, ?)"
		args = append(args, after.At, after.ID)
	}
	args = append(args, limit)

	rows, err := s.derived.QueryContext(ctx,
		`SELECT i.feed_id, i.title, i.link, i.published_at, 0, i.id
		   FROM items i
		  WHERE i.feed_id IN (`+marks+`)
		    AND NOT EXISTS (
		          SELECT 1 FROM read_articles r
		           WHERE r.principal_id = ? AND r.item_id = i.id)`+where+`
		  ORDER BY i.published_at DESC, i.id DESC
		  LIMIT ?`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("export unread articles: %w", err)
	}
	defer rows.Close()

	return scanExported(rows, "export unread articles")
}

// scanExported reads the six columns every article section selects, in the same order.
//
// The last is the row's own id, which never reaches the file: it is the second half of the
// keyset cursor, and an id is meaningless outside the instance that minted it.
func scanExported(rows *sql.Rows, what string) ([]ExportedArticle, error) {
	var out []ExportedArticle
	for rows.Next() {
		var (
			article ExportedArticle
			feedID  string
		)
		if err := rows.Scan(&feedID, &article.Title, &article.Link,
			&article.PublishedAt, &article.ReadAt, &article.ID); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		article.FeedID = feedID
		out = append(out, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

func orEmpty(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
