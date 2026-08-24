package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"bystander/internal/ids"
)

// TagFilter says how a page reads its list of tags.
type TagFilter string

// FeedFilter says how a page reads its list of feeds.
type FeedFilter string

const (
	// TagsIgnored means the tag list is not consulted, and the interface clears it.
	TagsIgnored TagFilter = "no"
	// TagsIncluding draws only from subscriptions carrying one of the listed tags.
	TagsIncluding TagFilter = "including"
	// TagsExcluding draws from everything except subscriptions carrying one of them.
	TagsExcluding TagFilter = "excluding"

	// FeedsAll means the feed list is not consulted.
	FeedsAll FeedFilter = "all"
	// FeedsIncluding draws only from the listed feeds.
	FeedsIncluding FeedFilter = "including"
	// FeedsExcluding draws from everything except the listed feeds.
	FeedsExcluding FeedFilter = "excluding"
)

// MainPageName is what a person's first page is called until they rename it — which they
// cannot, because it is the one page whose name and address are fixed.
const MainPageName = "Your page"

// MaxPageName is a limit on the tab, not on the idea. A name that does not fit in a tab strip
// is a name nobody can read anyway.
const MaxPageName = 60

// MaxPages is how many pages one person may keep.
//
// Generous rather than considered: this is here so a script cannot make a hundred thousand of
// them, not because anybody has an opinion about twenty. Each one is composed on its own clock
// and holds its own edition, so they are not free.
const MaxPages = 20

// Page is one front page: what it is called, where it lives, how often it is composed, and
// what it is allowed to draw from.
type Page struct {
	ID          string
	PrincipalID string
	Name        string
	// Slug is empty for the main page, which is served at / rather than at /f/:slug.
	Slug   string
	IsMain bool

	EditionInterval time.Duration
	EditionSize     int
	NextEditionAt   time.Time

	// ArticleWindow is how recent an article must be to reach this page. Zero is no limit.
	//
	// Over the top of each feed's own window, and the tighter of the two wins. The feed's says
	// how long that publisher stays worth reading; this says how current this page is meant to
	// be, and those are different questions.
	ArticleWindow time.Duration

	TagFilter  TagFilter
	FeedFilter FeedFilter
	// TagIDs and FeedIDs are the lists the filters read. Loaded only by the calls that say
	// they load them — the tab strip does not need them and the generator does.
	TagIDs  []string
	FeedIDs []string

	CreatedAt time.Time
}

// PagePatch is what a change to a page said. A nil field is one the request did not mention.
//
// Everything a page has in one patch, applied in one transaction, because the interface edits a
// page in a modal with a single save. Half a filter is a page drawing from the wrong things.
type PagePatch struct {
	Name            *string
	Slug            *string
	EditionInterval *time.Duration
	EditionSize     *int
	ArticleWindow   *time.Duration
	TagFilter       *TagFilter
	FeedFilter      *FeedFilter
	TagIDs          []string
	FeedIDs         []string
}

// MainPageID is the id of a principal's main page.
//
// Derived from the principal rather than minted, and this is the one id in the program that is.
// The reason is the gap between the two databases: derived.db holds editions and has to be able
// to name the page an existing edition belongs to, from the principal id it already has, without
// being able to join to a table in main.db. A rule both sides can apply is the only thing that
// works across that gap, and the migration that introduced pages applied exactly this rule.
//
// Only the main page. Every other page gets an ordinary random id, because nothing has to guess
// at those.
func MainPageID(principalID string) string { return ids.Page + principalID }

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Pages are one person's pages, main first and then oldest first.
//
// Main first because it is the first tab and cannot be moved. The rest by age, so a page does
// not jump around the strip when it is renamed.
func (s *Store) Pages(ctx context.Context, principalID string) ([]*Page, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+pageColumns+`
		   FROM pages WHERE principal_id = ?
		  ORDER BY is_main DESC, created_at, id`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Page
	for rows.Next() {
		page, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, page)
	}
	return out, rows.Err()
}

// PageByID returns one page, with the lists its filters read.
func (s *Store) PageByID(ctx context.Context, id string) (*Page, error) {
	page, err := scanPage(s.main.QueryRowContext(ctx,
		`SELECT `+pageColumns+` FROM pages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no page %s", id)
	}
	if err != nil {
		return nil, err
	}
	return page, s.loadPageLists(ctx, page)
}

// PageOf resolves one of a person's pages, by id or by address.
//
// Either, because both are how a page gets named: the interface addresses a page by its slug in
// a URL, and everything else holds its id. One lookup rather than two endpoints, and it settles
// ownership at the same time — a page belonging to somebody else is not found rather than
// forbidden, since whether a stranger has a page called "finances" is not this caller's
// business.
//
// An empty ref is the main page, which falls out of the query rather than being a special case:
// the main page's slug is the empty string.
func (s *Store) PageOf(ctx context.Context, principalID, ref string) (*Page, error) {
	page, err := scanPage(s.main.QueryRowContext(ctx,
		`SELECT `+pageColumns+`
		   FROM pages WHERE principal_id = ? AND (id = ? OR slug = ?)`,
		principalID, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		if ref == "" {
			// Every account is created with one, so this is a database somebody has edited
			// or a bug — either way not something to report as an ordinary missing page.
			return nil, NotFound("no main page for %s", principalID)
		}
		return nil, NotFound("no page %q", ref)
	}
	if err != nil {
		return nil, err
	}
	return page, s.loadPageLists(ctx, page)
}

// CreatePage adds a page. The filters start off consulting nothing, which is a page of
// everything — the same thing the main page is until somebody narrows it.
func (s *Store) CreatePage(ctx context.Context, principalID, name, slug string) (*Page, error) {
	name = strings.TrimSpace(name)
	if err := checkPageName(name); err != nil {
		return nil, err
	}
	if err := checkSlug(slug); err != nil {
		return nil, err
	}

	var count int
	if err := s.main.QueryRowContext(ctx,
		`SELECT count(*) FROM pages WHERE principal_id = ?`, principalID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxPages {
		return nil, Invalid("you can keep %d pages", MaxPages)
	}

	now := s.Now()
	page := &Page{
		ID:          ids.New(ids.Page),
		PrincipalID: principalID,
		Name:        name,
		Slug:        slug,
		// Now rather than now plus the interval, for the same reason a new account's is: an
		// empty page should fill on the next tick, not tomorrow.
		EditionInterval: 24 * time.Hour,
		EditionSize:     60,
		NextEditionAt:   now,
		TagFilter:       TagsIgnored,
		FeedFilter:      FeedsAll,
		CreatedAt:       now,
	}
	_, err := s.main.ExecContext(ctx,
		`INSERT INTO pages (id, principal_id, name, slug, is_main,
		                    edition_interval, edition_size, next_edition_at,
		                    tag_filter, feed_filter, created_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		page.ID, principalID, name, slug,
		int64(page.EditionInterval.Seconds()), page.EditionSize, unix(now),
		string(page.TagFilter), string(page.FeedFilter), unix(now))
	if err != nil {
		if isUnique(err) {
			return nil, Conflict("you already have a page at %q", slug)
		}
		return nil, err
	}
	return page, nil
}

// UpdatePage applies a change to a page and its lists, all or nothing.
//
// One transaction because the interface saves a page in one gesture. A page whose filter mode
// changed but whose list did not is a page drawing from the wrong things, and it would stay that
// way until somebody noticed and opened the modal again.
func (s *Store) UpdatePage(ctx context.Context, id string, patch PagePatch) error {
	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := scanPage(tx.QueryRowContext(ctx,
		`SELECT `+pageColumns+` FROM pages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no page %s", id)
	}
	if err != nil {
		return err
	}

	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		// The main page's name and address are fixed, so the interface shows no inputs for
		// them. Checked here too: an interface that shows no input is not a rule, it is the
		// absence of a way to break one.
		if current.IsMain {
			return Invalid("the main page cannot be renamed")
		}
		if err := checkPageName(name); err != nil {
			return err
		}
		current.Name = name
	}
	if patch.Slug != nil {
		if current.IsMain {
			return Invalid("the main page's address cannot be changed")
		}
		if err := checkSlug(*patch.Slug); err != nil {
			return err
		}
		current.Slug = *patch.Slug
	}
	if patch.EditionInterval != nil {
		if !validInterval(*patch.EditionInterval) {
			return Invalid("%s is not one of the intervals a page can be composed on", *patch.EditionInterval)
		}
		// Rebased from when this page was last due rather than from now, so shortening the
		// interval takes effect at once instead of after one more of the old ones. Clamped
		// forward, so a week shortened to an hour does not leave a page six days overdue and
		// compose the moment it is saved.
		last := current.NextEditionAt.Add(-current.EditionInterval)
		next := last.Add(*patch.EditionInterval)
		if now := s.Now(); next.Before(now) {
			next = now
		}
		current.EditionInterval = *patch.EditionInterval
		current.NextEditionAt = next
	}
	if patch.EditionSize != nil {
		if *patch.EditionSize < MinEditionSize || *patch.EditionSize > MaxEditionSize {
			return Invalid("a page holds between %d and %d articles", MinEditionSize, MaxEditionSize)
		}
		current.EditionSize = *patch.EditionSize
	}
	if patch.ArticleWindow != nil {
		if !validWindow(*patch.ArticleWindow) {
			return Invalid("%s is not one of the windows a page can be held to", *patch.ArticleWindow)
		}
		current.ArticleWindow = *patch.ArticleWindow
	}
	if patch.TagFilter != nil {
		if !validTagFilter(*patch.TagFilter) {
			return Invalid("%q is not a way of filtering by tag", *patch.TagFilter)
		}
		current.TagFilter = *patch.TagFilter
	}
	if patch.FeedFilter != nil {
		if !validFeedFilter(*patch.FeedFilter) {
			return Invalid("%q is not a way of filtering by feed", *patch.FeedFilter)
		}
		current.FeedFilter = *patch.FeedFilter
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET name = ?, slug = ?, edition_interval = ?, edition_size = ?,
		                  next_edition_at = ?, max_article_age = ?, tag_filter = ?, feed_filter = ?
		  WHERE id = ?`,
		current.Name, current.Slug, int64(current.EditionInterval.Seconds()), current.EditionSize,
		unix(current.NextEditionAt), int64(current.ArticleWindow.Seconds()),
		string(current.TagFilter), string(current.FeedFilter), id); err != nil {
		if isUnique(err) {
			return Conflict("you already have a page at %q", current.Slug)
		}
		return err
	}

	// A mode that consults no list empties it, rather than leaving a list nobody reads to
	// reappear the next time the mode changes. What somebody last chose while the filter was
	// off is not what they mean when they turn it back on.
	if current.TagFilter == TagsIgnored {
		patch.TagIDs = []string{}
	}
	if current.FeedFilter == FeedsAll {
		patch.FeedIDs = []string{}
	}
	if patch.TagIDs != nil {
		if err := replacePageList(ctx, tx, "page_tags", "tag_id", id, patch.TagIDs); err != nil {
			return err
		}
	}
	if patch.FeedIDs != nil {
		if err := replacePageList(ctx, tx, "page_feeds", "feed_id", id, patch.FeedIDs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeletePage removes a page and, by cascade, its filter lists. The main page stays.
//
// The edition it was showing is left to the derived database's own tidying, which already
// deletes editions belonging to pages that are gone.
func (s *Store) DeletePage(ctx context.Context, id string) error {
	var isMain bool
	err := s.main.QueryRowContext(ctx, `SELECT is_main FROM pages WHERE id = ?`, id).Scan(&isMain)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no page %s", id)
	}
	if err != nil {
		return err
	}
	if isMain {
		return Invalid("the main page cannot be removed")
	}

	res, err := s.main.ExecContext(ctx, `DELETE FROM pages WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no page %s", id))
}

// DuePages are the pages ready to be composed, oldest due first.
//
// Disabled accounts are skipped here rather than by the caller, because a page is due whether
// or not anybody may still look at it and this is the only query that knows about both.
func (s *Store) DuePages(ctx context.Context) ([]*Page, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+qualify(pageColumns, "g")+`
		   FROM pages g
		   JOIN principals p ON p.id = g.principal_id
		  WHERE g.next_edition_at <= ? AND p.disabled_at IS NULL
		  ORDER BY g.next_edition_at`,
		unix(s.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Page
	for rows.Next() {
		page, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, page)
	}
	return out, rows.Err()
}

// LivePageIDs is every page that still exists, for the derived database's tidying.
func (s *Store) LivePageIDs(ctx context.Context) ([]string, error) {
	rows, err := s.main.QueryContext(ctx, `SELECT id FROM pages`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ScheduleNextEdition moves one page's clock forward.
func (s *Store) ScheduleNextEdition(ctx context.Context, pageID string, at time.Time) error {
	_, err := s.main.ExecContext(ctx,
		`UPDATE pages SET next_edition_at = ? WHERE id = ?`, unix(at), pageID)
	return err
}

const pageColumns = `id, principal_id, name, slug, is_main, edition_interval, edition_size, next_edition_at, max_article_age, tag_filter, feed_filter, created_at`

func scanPage(row interface{ Scan(...any) error }) (*Page, error) {
	var (
		page     Page
		isMain   int
		interval int64
		next     int64
		window   int64
		created  int64
		tagMode  string
		feedMode string
	)
	if err := row.Scan(&page.ID, &page.PrincipalID, &page.Name, &page.Slug, &isMain,
		&interval, &page.EditionSize, &next, &window, &tagMode, &feedMode, &created); err != nil {
		return nil, err
	}
	page.IsMain = isMain == 1
	page.EditionInterval = time.Duration(interval) * time.Second
	page.ArticleWindow = time.Duration(window) * time.Second
	page.NextEditionAt = time.Unix(next, 0).UTC()
	page.TagFilter = TagFilter(tagMode)
	page.FeedFilter = FeedFilter(feedMode)
	page.CreatedAt = time.Unix(created, 0).UTC()
	return &page, nil
}

// loadPageLists fills in the tags and feeds a page's filters read.
func (s *Store) loadPageLists(ctx context.Context, page *Page) error {
	var err error
	if page.TagIDs, err = s.pageList(ctx, "page_tags", "tag_id", page.ID); err != nil {
		return err
	}
	page.FeedIDs, err = s.pageList(ctx, "page_feeds", "feed_id", page.ID)
	return err
}

func (s *Store) pageList(ctx context.Context, table, column, pageID string) ([]string, error) {
	// The table and column are constants at every call site, never anything a request said.
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+column+` FROM `+table+` WHERE page_id = ? ORDER BY `+column, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// replacePageList sets one of a page's lists to exactly what was given.
//
// Delete then insert, rather than working out a difference. The list is a handful of rows and
// the request already carries the whole of what it should be, so a diff would be arithmetic in
// aid of nothing.
func replacePageList(ctx context.Context, tx *sql.Tx, table, column, pageID string, ids []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE page_id = ?`, pageID); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO `+table+` (page_id, `+column+`) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, pageID, id); err != nil {
			// A tag or feed that is not this person's, or not there at all. Refused rather
			// than ignored: a filter quietly missing one of the things it was told about is
			// a page drawing from the wrong set and saying nothing.
			return Invalid("%s is not something this page can filter by", id)
		}
	}
	return nil
}

// qualify prefixes a column list with a table alias, for a query that joins.
func qualify(columns, alias string) string {
	parts := strings.Split(columns, ", ")
	for i, part := range parts {
		parts[i] = alias + "." + part
	}
	return strings.Join(parts, ", ")
}

func checkPageName(name string) error {
	if name == "" {
		return Invalid("a page needs a name")
	}
	if len([]rune(name)) > MaxPageName {
		return Invalid("a page's name is at most %d characters", MaxPageName)
	}
	return nil
}

// checkSlug holds a page's address to what can appear in a URL without being escaped.
//
// Non-empty, because an empty slug is the main page's and is not something anybody can ask for.
func checkSlug(slug string) error {
	if slug == "" {
		return Invalid("a page needs an address")
	}
	if len(slug) > 40 {
		return Invalid("a page's address is at most 40 characters")
	}
	if !slugPattern.MatchString(slug) {
		return Invalid("a page's address may use lowercase letters, numbers and hyphens")
	}
	return nil
}

func validTagFilter(f TagFilter) bool {
	return f == TagsIgnored || f == TagsIncluding || f == TagsExcluding
}

func validFeedFilter(f FeedFilter) bool {
	return f == FeedsAll || f == FeedsIncluding || f == FeedsExcluding
}
