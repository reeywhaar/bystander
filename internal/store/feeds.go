package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"bystander/internal/ids"
)

// Feed is a URL that publishes items, shared by everybody who follows it.
//
// Everything here is what the fetcher learned. What a person chose lives on Subscription.
type Feed struct {
	ID           string
	URL          string // as entered, so an error can quote it back
	CanonicalURL string // the dedup key
	Title        string
	SiteURL      string

	ETag         string
	LastModified string

	LastFetchAt   time.Time
	LastSuccessAt time.Time
	LastStatus    int
	LastError     string
	FailureCount  int
	NextFetchAt   time.Time

	CreatedAt time.Time
}

// Subscription is one person's relationship to a feed.
type Subscription struct {
	ID            string
	PrincipalID   string
	FeedID        string
	TitleOverride string
	Priority      int

	// ArticleWindow is how old an article from this feed may be and still reach a page.
	// Zero is no limit.
	//
	// Per feed rather than per person, because that is the shape of the question: a news
	// feed worth a day and a blog worth a year are exactly the pair one number cannot
	// serve.
	ArticleWindow time.Duration

	CreatedAt time.Time

	Feed   *Feed
	TagIDs []string
}

// Title is what to call this feed on this person's page: their name for it if they gave
// one, the publisher's otherwise, and the URL if the publisher gave none either.
func (s *Subscription) Title() string {
	switch {
	case s.TitleOverride != "":
		return s.TitleOverride
	case s.Feed != nil && s.Feed.Title != "":
		return s.Feed.Title
	case s.Feed != nil:
		return s.Feed.URL
	default:
		return ""
	}
}

// CanonicalURL normalises a feed URL into the key two subscribers to the same feed will
// agree on: scheme and host lowercased, the default port dropped, the fragment dropped,
// an empty path written as "/".
//
// The query is kept. It is routinely load-bearing — ?format=rss, ?tag=go — and dropping
// it would collapse feeds that are genuinely different.
func CanonicalURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", Invalid("that is not a URL")
	}
	// A bare hostname is what people paste. Assuming https rather than refusing turns a
	// dead end into a fetch that will say something useful.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", Invalid("%q is not a URL", raw)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", Invalid("a feed is http or https, not %q", u.Scheme)
	}
	if u.Host == "" {
		return "", Invalid("%q names no host", raw)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = u.Host[:strings.LastIndex(u.Host, ":")]
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.Fragment = ""
	return u.String(), nil
}

// UpsertFeed returns the feed for a URL, creating it if nobody follows it yet.
//
// Feeds are global: two people following the same URL cause one fetch, which matters to
// the publisher more than to us and keeps the poller's work proportional to distinct URLs.
func (s *Store) UpsertFeed(ctx context.Context, rawURL, title, siteURL string) (*Feed, error) {
	canonical, err := CanonicalURL(rawURL)
	if err != nil {
		return nil, err
	}

	if feed, err := s.feedBy(ctx, `canonical_url = ?`, canonical); err == nil {
		return feed, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	feed := &Feed{
		ID:           ids.New(ids.Feed),
		URL:          strings.TrimSpace(rawURL),
		CanonicalURL: canonical,
		Title:        title,
		SiteURL:      siteURL,
		CreatedAt:    s.Now(),
		// Due immediately: somebody who has just added a feed is waiting to see it.
		NextFetchAt: s.Now(),
	}
	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO feeds (id, url, canonical_url, title, site_url, next_fetch_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		feed.ID, feed.URL, feed.CanonicalURL, feed.Title, feed.SiteURL,
		unix(feed.NextFetchAt), unix(feed.CreatedAt)); err != nil {
		// Two people adding the same feed at the same moment. Whoever lost the race
		// wanted the row that now exists.
		if isUnique(err) {
			return s.feedBy(ctx, `canonical_url = ?`, canonical)
		}
		return nil, fmt.Errorf("create feed: %w", err)
	}
	return feed, nil
}

const feedColumns = `id, url, canonical_url, title, site_url, etag, last_modified,
	last_fetch_at, last_success_at, last_status, last_error, failure_count, next_fetch_at, created_at`

func scanFeed(row interface{ Scan(...any) error }) (*Feed, error) {
	var (
		f         Feed
		lastFetch sql.NullInt64
		lastOK    sql.NullInt64
		status    sql.NullInt64
		next      int64
		created   int64
	)
	if err := row.Scan(&f.ID, &f.URL, &f.CanonicalURL, &f.Title, &f.SiteURL, &f.ETag, &f.LastModified,
		&lastFetch, &lastOK, &status, &f.LastError, &f.FailureCount, &next, &created); err != nil {
		return nil, err
	}
	f.LastFetchAt = timeFrom(lastFetch)
	f.LastSuccessAt = timeFrom(lastOK)
	f.LastStatus = int(status.Int64)
	f.NextFetchAt = time.Unix(next, 0).UTC()
	f.CreatedAt = time.Unix(created, 0).UTC()
	return &f, nil
}

func (s *Store) feedBy(ctx context.Context, where string, arg any) (*Feed, error) {
	f, err := scanFeed(s.main.QueryRowContext(ctx, `SELECT `+feedColumns+` FROM feeds WHERE `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no such feed")
	}
	return f, err
}

// FeedByID returns one feed.
func (s *Store) FeedByID(ctx context.Context, id string) (*Feed, error) {
	return s.feedBy(ctx, `id = ?`, id)
}

// DueFeeds returns the feeds whose next fetch has come, oldest first.
func (s *Store) DueFeeds(ctx context.Context, limit int) ([]*Feed, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM feeds WHERE next_fetch_at <= ? ORDER BY next_fetch_at LIMIT ?`,
		unix(s.Now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RecordSuccess stamps a fetch that worked and clears the failure state.
func (s *Store) RecordSuccess(ctx context.Context, feedID, title, siteURL, etag, lastModified string, status int, next time.Time) error {
	now := unix(s.Now())
	_, err := s.main.ExecContext(ctx,
		`UPDATE feeds SET
		   title = CASE WHEN ? <> '' THEN ? ELSE title END,
		   site_url = CASE WHEN ? <> '' THEN ? ELSE site_url END,
		   etag = ?, last_modified = ?,
		   last_fetch_at = ?, last_success_at = ?, last_status = ?,
		   last_error = '', failure_count = 0, next_fetch_at = ?
		 WHERE id = ?`,
		title, title, siteURL, siteURL, etag, lastModified, now, now, status, unix(next), feedID)
	return err
}

// RecordFailure stamps a fetch that did not work and advances the failure count that
// drives backoff.
//
// The title and the last successful fetch are left alone: a feed that broke this morning
// should still show its name and when it last worked, which is the pair of facts somebody
// looking at the manage page actually needs.
func (s *Store) RecordFailure(ctx context.Context, feedID string, status int, message string, next time.Time) error {
	_, err := s.main.ExecContext(ctx,
		`UPDATE feeds SET last_fetch_at = ?, last_status = ?, last_error = ?,
		   failure_count = failure_count + 1, next_fetch_at = ?
		 WHERE id = ?`,
		unix(s.Now()), status, message, unix(next), feedID)
	return err
}

// DeleteOrphanFeeds removes feeds nobody follows any more, and reports how many went.
// Their items are collected separately, in the other database.
func (s *Store) DeleteOrphanFeeds(ctx context.Context) (int64, error) {
	res, err := s.main.ExecContext(ctx,
		`DELETE FROM feeds WHERE id NOT IN (SELECT feed_id FROM subscriptions)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FeedIDs returns every feed id, for the sweep that collects items belonging to feeds that
// no longer exist.
func (s *Store) FeedIDs(ctx context.Context) ([]string, error) {
	rows, err := s.main.QueryContext(ctx, `SELECT id FROM feeds`)
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

// Subscribe attaches a principal to a feed.
func (s *Store) Subscribe(ctx context.Context, principalID, feedID string, priority int, tagIDs []string) (*Subscription, error) {
	if err := validatePriority(priority); err != nil {
		return nil, err
	}

	sub := &Subscription{
		ID:            ids.New(ids.Subscription),
		PrincipalID:   principalID,
		FeedID:        feedID,
		Priority:      priority,
		ArticleWindow: DefaultArticleWindow,
		CreatedAt:     s.Now(),
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (id, principal_id, feed_id, priority, max_article_age, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sub.ID, principalID, feedID, priority,
		int64(sub.ArticleWindow.Seconds()), unix(sub.CreatedAt)); err != nil {
		if isUnique(err) {
			return nil, Conflict("you already follow that feed")
		}
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	if err := setSubscriptionTags(ctx, tx, principalID, sub.ID, tagIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	sub.TagIDs = tagIDs
	sub.Feed, err = s.FeedByID(ctx, feedID)
	return sub, err
}

// setSubscriptionTags replaces the tags on a subscription.
//
// The INSERT ... SELECT is what scopes it: a tag id that belongs to somebody else matches
// no row and is silently dropped rather than attached. Checking afterwards would be a
// second query and a race.
func setSubscriptionTags(ctx context.Context, tx *sql.Tx, principalID, subscriptionID string, tagIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_tags WHERE subscription_id = ?`, subscriptionID); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO subscription_tags (subscription_id, tag_id)
			 SELECT ?, id FROM tags WHERE id = ? AND principal_id = ?`,
			subscriptionID, tagID, principalID); err != nil {
			return err
		}
	}
	return nil
}

const subscriptionColumns = `s.id, s.principal_id, s.feed_id, s.title_override, s.priority,
	s.max_article_age, s.created_at`

func scanSubscription(row interface{ Scan(...any) error }) (*Subscription, error) {
	var (
		sub     Subscription
		window  int64
		created int64
		feed    Feed

		lastFetch sql.NullInt64
		lastOK    sql.NullInt64
		status    sql.NullInt64
		next      int64
		feedMade  int64
	)
	if err := row.Scan(&sub.ID, &sub.PrincipalID, &sub.FeedID, &sub.TitleOverride, &sub.Priority,
		&window, &created,
		&feed.ID, &feed.URL, &feed.CanonicalURL, &feed.Title, &feed.SiteURL, &feed.ETag, &feed.LastModified,
		&lastFetch, &lastOK, &status, &feed.LastError, &feed.FailureCount, &next, &feedMade); err != nil {
		return nil, err
	}
	sub.ArticleWindow = time.Duration(window) * time.Second
	sub.CreatedAt = time.Unix(created, 0).UTC()
	feed.LastFetchAt = timeFrom(lastFetch)
	feed.LastSuccessAt = timeFrom(lastOK)
	feed.LastStatus = int(status.Int64)
	feed.NextFetchAt = time.Unix(next, 0).UTC()
	feed.CreatedAt = time.Unix(feedMade, 0).UTC()
	sub.Feed = &feed
	return &sub, nil
}

const subscriptionSelect = `SELECT ` + subscriptionColumns + `,
	f.id, f.url, f.canonical_url, f.title, f.site_url, f.etag, f.last_modified,
	f.last_fetch_at, f.last_success_at, f.last_status, f.last_error, f.failure_count,
	f.next_fetch_at, f.created_at
	FROM subscriptions s JOIN feeds f ON f.id = s.feed_id`

// ListSubscriptions returns everything a principal follows, with each feed's state and
// tags attached.
func (s *Store) ListSubscriptions(ctx context.Context, principalID string) ([]*Subscription, error) {
	rows, err := s.main.QueryContext(ctx,
		subscriptionSelect+` WHERE s.principal_id = ? ORDER BY s.id DESC`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Subscription
	byID := make(map[string]*Subscription)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
		byID[sub.ID] = sub
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Tags in a second query rather than a join. Joining would multiply every
	// subscription row by its tag count and leave this loop de-duplicating feeds it had
	// already scanned.
	tagRows, err := s.main.QueryContext(ctx,
		// Ordered the same way ListTags orders them, so a feed's tags read in the same
		// sequence wherever they appear. Unordered, the row under a feed said "Tech ·
		// Design" while the dialog showed the same two as "Design, Tech" — the same
		// answer twice, in two different orders, which reads as two different answers.
		`SELECT st.subscription_id, st.tag_id
		   FROM subscription_tags st
		   JOIN subscriptions s ON s.id = st.subscription_id
		   JOIN tags t ON t.id = st.tag_id
		  WHERE s.principal_id = ?
		  ORDER BY ifnull(t.parent_id,''), t.name`, principalID)
	if err != nil {
		return nil, err
	}
	defer tagRows.Close()

	for tagRows.Next() {
		var subID, tagID string
		if err := tagRows.Scan(&subID, &tagID); err != nil {
			return nil, err
		}
		if sub := byID[subID]; sub != nil {
			sub.TagIDs = append(sub.TagIDs, tagID)
		}
	}
	return out, tagRows.Err()
}

// SubscriptionByID returns one of this principal's subscriptions.
func (s *Store) SubscriptionByID(ctx context.Context, principalID, id string) (*Subscription, error) {
	sub, err := scanSubscription(s.main.QueryRowContext(ctx,
		subscriptionSelect+` WHERE s.id = ? AND s.principal_id = ?`, id, principalID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no subscription %s", id)
	}
	if err != nil {
		return nil, err
	}

	tagRows, err := s.main.QueryContext(ctx,
		`SELECT st.tag_id
		   FROM subscription_tags st
		   JOIN tags t ON t.id = st.tag_id
		  WHERE st.subscription_id = ?
		  ORDER BY ifnull(t.parent_id,''), t.name`, id)
	if err != nil {
		return nil, err
	}
	defer tagRows.Close()
	for tagRows.Next() {
		var tagID string
		if err := tagRows.Scan(&tagID); err != nil {
			return nil, err
		}
		sub.TagIDs = append(sub.TagIDs, tagID)
	}
	return sub, tagRows.Err()
}

// SubscriptionPatch is what a PATCH said. A nil field is one the request did not mention.
type SubscriptionPatch struct {
	Priority      *int
	TitleOverride *string
	TagIDs        *[]string
	ArticleWindow *time.Duration
}

// UpdateSubscription changes what was passed and leaves the rest alone.
func (s *Store) UpdateSubscription(ctx context.Context, principalID, id string, patch SubscriptionPatch) error {
	sub, err := s.SubscriptionByID(ctx, principalID, id)
	if err != nil {
		return err
	}
	if patch.Priority != nil {
		if err := validatePriority(*patch.Priority); err != nil {
			return err
		}
		sub.Priority = *patch.Priority
	}
	if patch.TitleOverride != nil {
		sub.TitleOverride = strings.TrimSpace(*patch.TitleOverride)
	}
	if patch.ArticleWindow != nil {
		if !validWindow(*patch.ArticleWindow) {
			return Invalid("%s is not one of the windows an article can be picked from", *patch.ArticleWindow)
		}
		sub.ArticleWindow = *patch.ArticleWindow
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE subscriptions SET priority = ?, title_override = ?, max_article_age = ?
		 WHERE id = ? AND principal_id = ?`,
		sub.Priority, sub.TitleOverride, int64(sub.ArticleWindow.Seconds()), id, principalID)
	if err != nil {
		return err
	}
	if err := expectOne(res, NotFound("no subscription %s", id)); err != nil {
		return err
	}
	if patch.TagIDs != nil {
		if err := setSubscriptionTags(ctx, tx, principalID, id, *patch.TagIDs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteSubscription stops a principal following a feed. The feed itself survives if
// somebody else follows it, and is collected by the sweep if nobody does.
func (s *Store) DeleteSubscription(ctx context.Context, principalID, id string) error {
	res, err := s.main.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE id = ? AND principal_id = ?`, id, principalID)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no subscription %s", id))
}
