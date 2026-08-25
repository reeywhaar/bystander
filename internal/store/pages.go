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

// MainPageName is what the one page everybody has is called.
//
// A name rather than a description, and capitalised for it. "Your page" was neither: it read as
// a label on a settings screen, and once a person had several it stopped being true — they are
// all your pages. A newspaper's front page is *the* front page and its other sections are
// called something, which is exactly the shape of this.
//
// See private/docs/conventions.md for front page, Front Page and edition, which are three
// different things.
const MainPageName = "Front Page"

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

	// The tags are a funnel, and the feeds override what comes out of it. Loaded only by the
	// calls that say they load the lists — the tab strip does not need them and the generator
	// does. A tag or feed the page has no opinion about is on neither list, which is the
	// ordinary case and why both stay short.
	//
	// IncludeTagIDs, when it has anything in it, holds the page to subscriptions carrying at
	// least one of these. Empty means the page was never narrowed this way, rather than
	// narrowed to nothing.
	IncludeTagIDs []string
	// ExcludeTagIDs drops subscriptions carrying any of these, after the include has run.
	// After, because that ordering is the whole point of having both: tags overlap, and
	// "Finance, but not the feeds that are also Crypto" needs the include to have happened
	// first.
	ExcludeTagIDs []string

	// PublishSlug is where this page lives on the open web, under its owner's public name:
	// /p/<their name>/<this>. Kept when a page is taken down rather than cleared, so that
	// publishing it again offers the address the links already point at.
	//
	// Separate from Slug, and it has to be: the main page has no Slug at all — it is served
	// at / rather than at /f/:slug — and it is the page most people would publish first.
	PublishSlug string
	// Published is whether that address answers. Its own field rather than an empty slug,
	// so taking a page down does not throw away where it was.
	Published bool
	// Indexable is the owner's answer to "should a search engine keep this". The instance's
	// answer overrules it — see [InstanceSettings] — and where the instance says no this is
	// never true on the way out, whatever is stored.
	Indexable bool

	// IncludeFeedIDs are on the page whatever the tags decided, and ExcludeFeedIDs are off it
	// whatever they decided. An override rather than a second funnel, which is the difference
	// between what somebody wants to say — "this one as well", "this one never" — and what a
	// narrowing filter could express.
	IncludeFeedIDs []string
	ExcludeFeedIDs []string

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
	// A nil list is one the request did not mention; an empty non-nil list is one it emptied.
	IncludeTagIDs  []string
	ExcludeTagIDs  []string
	IncludeFeedIDs []string
	ExcludeFeedIDs []string
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
	return page, loadPageLists(ctx, s.main, page)
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
	return page, loadPageLists(ctx, s.main, page)
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
		// Sixty, and it is a starting point rather than a recommendation.
		//
		// Measured against a real instance: nineteen feeds publish about eighty-five articles
		// a day, and the two pages there had been moved to ninety and thirty — nobody sat on
		// the default, and both editions filled to exactly their ceiling. So a daily page for
		// somebody who reads a lot wants more than this, and a filtered one wants less.
		//
		// It stays low anyway. Too small is a slider somebody moves once; too large is a page
		// that looks like a feed reader, which is the thing this is not. And it costs nothing
		// to be shy: the size is a ceiling and never a quota, so a new account following four
		// blogs gets the four blogs rather than sixty slots of disappointment.
		EditionSize:   60,
		NextEditionAt: now,
		CreatedAt:     now,
	}
	_, err := s.main.ExecContext(ctx,
		`INSERT INTO pages (id, principal_id, name, slug, is_main,
		                    edition_interval, edition_size, next_edition_at, created_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		page.ID, principalID, name, slug,
		int64(page.EditionInterval.Seconds()), page.EditionSize, unix(now), unix(now))
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

	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET name = ?, slug = ?, edition_interval = ?, edition_size = ?,
		                  next_edition_at = ?, max_article_age = ?
		  WHERE id = ?`,
		current.Name, current.Slug, int64(current.EditionInterval.Seconds()), current.EditionSize,
		unix(current.NextEditionAt), int64(current.ArticleWindow.Seconds()), id); err != nil {
		if isUnique(err) {
			return Conflict("you already have a page at %q", current.Slug)
		}
		return err
	}

	// A tag or feed on both sides is a contradiction rather than a filter with an unlucky
	// answer, so it is refused here — where both halves of the patch are in hand — rather than
	// left to the primary key, which would silently drop whichever arrived second.
	//
	// Checked against what the page will hold rather than against what the request mentioned:
	// a request that only sets one side still has to agree with the other side already saved.
	var loaded bool
	sides := func(include, exclude []string, was func() ([]string, []string)) ([]string, []string, error) {
		if include != nil && exclude != nil {
			return include, exclude, nil
		}
		if !loaded {
			if err := loadPageLists(ctx, tx, current); err != nil {
				return nil, nil, err
			}
			loaded = true
		}
		heldInclude, heldExclude := was()
		if include == nil {
			include = heldInclude
		}
		if exclude == nil {
			exclude = heldExclude
		}
		return include, exclude, nil
	}

	tagsIn, tagsOut, err := sides(patch.IncludeTagIDs, patch.ExcludeTagIDs,
		func() ([]string, []string) { return current.IncludeTagIDs, current.ExcludeTagIDs })
	if err != nil {
		return err
	}
	if err := noBothSides(tagsIn, tagsOut, "tag"); err != nil {
		return err
	}

	feedsIn, feedsOut, err := sides(patch.IncludeFeedIDs, patch.ExcludeFeedIDs,
		func() ([]string, []string) { return current.IncludeFeedIDs, current.ExcludeFeedIDs })
	if err != nil {
		return err
	}
	if err := noBothSides(feedsIn, feedsOut, "feed"); err != nil {
		return err
	}

	for _, list := range []struct {
		table, column, mode string
		ids                 []string
	}{
		{"page_tags", "tag_id", "include", patch.IncludeTagIDs},
		{"page_tags", "tag_id", "exclude", patch.ExcludeTagIDs},
		{"page_feeds", "feed_id", "include", patch.IncludeFeedIDs},
		{"page_feeds", "feed_id", "exclude", patch.ExcludeFeedIDs},
	} {
		if list.ids == nil {
			continue
		}
		if err := replacePageList(ctx, tx, list.table, list.column, id, list.mode, list.ids); err != nil {
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
		`SELECT `+prefixed(pageColumns, "g")+`
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

const pageColumns = `id, principal_id, name, slug, is_main, edition_interval, edition_size, next_edition_at, max_article_age, publish_slug, published, indexable, created_at`

func scanPage(row interface{ Scan(...any) error }) (*Page, error) {
	var (
		page     Page
		isMain   int
		interval int64
		next     int64
		window   int64
		created  int64
	)
	if err := row.Scan(&page.ID, &page.PrincipalID, &page.Name, &page.Slug, &isMain,
		&interval, &page.EditionSize, &next, &window,
		&page.PublishSlug, &page.Published, &page.Indexable, &created); err != nil {
		return nil, err
	}
	page.IsMain = isMain == 1
	page.EditionInterval = time.Duration(interval) * time.Second
	page.ArticleWindow = time.Duration(window) * time.Second
	page.NextEditionAt = time.Unix(next, 0).UTC()
	page.CreatedAt = time.Unix(created, 0).UTC()
	return &page, nil
}

// reader is whatever a query can be asked through: the database, or a transaction on it.
//
// It exists because there is exactly one connection to each database — see store.Open — so a
// read issued against the pool while a transaction holds that connection waits for a connection
// the transaction will not give up until it commits, and the commit is waiting on the read.
// That is a deadlock with no timeout and no error, and the only symptom is a request that never
// returns. Passing the transaction in is what stops the question arising.
type reader interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// loadPageLists fills in the tags and feeds a page's filter names, side by side.
func loadPageLists(ctx context.Context, db reader, page *Page) error {
	var err error
	if page.IncludeTagIDs, err = pageList(ctx, db, "page_tags", "tag_id", page.ID, "include"); err != nil {
		return err
	}
	if page.ExcludeTagIDs, err = pageList(ctx, db, "page_tags", "tag_id", page.ID, "exclude"); err != nil {
		return err
	}
	if page.IncludeFeedIDs, err = pageList(ctx, db, "page_feeds", "feed_id", page.ID, "include"); err != nil {
		return err
	}
	page.ExcludeFeedIDs, err = pageList(ctx, db, "page_feeds", "feed_id", page.ID, "exclude")
	return err
}

// pageList is one side of one of a page's two filter lists.
func pageList(ctx context.Context, db reader, table, column, pageID, mode string) ([]string, error) {
	// The table and column are constants at every call site, never anything a request said.
	rows, err := db.QueryContext(ctx,
		`SELECT `+column+` FROM `+table+` WHERE page_id = ? AND mode = ? ORDER BY `+column,
		pageID, mode)
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

// replacePageList sets one side of one of a page's lists to exactly what was given, leaving the
// other side alone.
//
// The mode in the delete is what makes that true. Both sides live in one table, so without it
// saving the drop side would empty the draw-from side — and the page would silently widen to
// everything, which is the opposite of what the person pressing save was doing.
//
// Delete then insert, rather than working out a difference. The list is a handful of rows and
// the request already carries the whole of what it should be, so a diff would be arithmetic in
// aid of nothing.
func replacePageList(ctx context.Context, tx *sql.Tx, table, column, pageID, mode string, ids []string) error {
	// The table and column are constants at every call site, never anything a request said.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM `+table+` WHERE page_id = ? AND mode = ?`, pageID, mode); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO `+table+` (page_id, `+column+`, mode) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, pageID, id, mode); err != nil {
			// A tag or feed that is not this person's, or not there at all. Refused rather
			// than ignored: a filter quietly missing one of the things it was told about is
			// a page drawing from the wrong set and saying nothing.
			return Invalid("%s is not something this page can filter by", id)
		}
	}
	return nil
}

// noBothSides refuses a tag or feed that is on both sides at once.
func noBothSides(include, exclude []string, what string) error {
	if len(include) == 0 || len(exclude) == 0 {
		return nil
	}
	on := make(map[string]bool, len(include))
	for _, id := range include {
		on[id] = true
	}
	for _, id := range exclude {
		if on[id] {
			return Invalid("a page cannot both take a %s and drop it", what)
		}
	}
	return nil
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
