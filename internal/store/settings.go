package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The intervals an edition can be generated on.
//
// A closed set rather than an arbitrary schedule: a cron expression is a support burden
// with no matching demand, and four options fit in a segmented control. The CHECK
// constraint in the schema holds the same list, so a value that got past this would still
// be refused by the database.
var EditionIntervals = []time.Duration{
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
	7 * 24 * time.Hour,
}

// Bounds on how many articles a page holds.
const (
	MinEditionSize = 10
	MaxEditionSize = 200
)

// ArticleWindows are how recent an article must be to reach a page, in the order the
// interface offers them. Zero is no limit.
//
// A closed set for the same reason the intervals are: four or five choices fit in a
// control, and an arbitrary duration is a support burden nobody asked for. The schema
// holds the same list in a CHECK, so a value that got past this would still be refused.
var ArticleWindows = []time.Duration{
	0,
	365 * 24 * time.Hour,
	30 * 24 * time.Hour,
	14 * 24 * time.Hour,
	7 * 24 * time.Hour,
	24 * time.Hour,
}

// DefaultArticleWindow is a week. A front page is about what is going on, and a
// fortnight-old article on it is a different kind of object.
const DefaultArticleWindow = 7 * 24 * time.Hour

// Settings is one person's edition preferences.
type Settings struct {
	PrincipalID     string
	EditionInterval time.Duration
	EditionSize     int

	NextEditionAt time.Time
}

// SettingsPatch is what a PATCH said. A nil field is one the request did not mention.
type SettingsPatch struct {
	EditionInterval *time.Duration
	EditionSize     *int
}

// Settings returns one principal's preferences. The row is created with the account, so
// this is only ErrNotFound when the account is.
func (s *Store) Settings(ctx context.Context, principalID string) (*Settings, error) {
	var (
		set      = Settings{PrincipalID: principalID}
		interval int64
		next     int64
	)
	err := s.main.QueryRowContext(ctx,
		`SELECT edition_interval, edition_size, next_edition_at
		   FROM settings WHERE principal_id = ?`,
		principalID).Scan(&interval, &set.EditionSize, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no settings for %s", principalID)
	}
	if err != nil {
		return nil, err
	}
	set.EditionInterval = time.Duration(interval) * time.Second
	set.NextEditionAt = time.Unix(next, 0).UTC()
	return &set, nil
}

// UpdateSettings changes what was passed and leaves the rest alone. A nil field means "not
// mentioned", which is what a PATCH body means.
func (s *Store) UpdateSettings(ctx context.Context, principalID string, patch SettingsPatch) error {
	interval, size := patch.EditionInterval, patch.EditionSize
	current, err := s.Settings(ctx, principalID)
	if err != nil {
		return err
	}

	next := current.NextEditionAt
	if interval != nil {
		if !validInterval(*interval) {
			return Invalid("%s is not one of the intervals a page can be generated on", *interval)
		}
		// Rebase from the last time a page was due rather than from now, so switching
		// daily to hourly does not mean waiting a further hour for the change to mean
		// anything. Clamped forward, so a long interval shortened does not leave a page
		// overdue by six days and regenerate immediately.
		last := current.NextEditionAt.Add(-current.EditionInterval)
		next = last.Add(*interval)
		if now := s.Now(); next.Before(now) {
			next = now
		}
		current.EditionInterval = *interval
	}
	if size != nil {
		if *size < MinEditionSize || *size > MaxEditionSize {
			return Invalid("a page holds between %d and %d articles", MinEditionSize, MaxEditionSize)
		}
		current.EditionSize = *size
	}

	res, err := s.main.ExecContext(ctx,
		`UPDATE settings SET edition_interval = ?, edition_size = ?, next_edition_at = ?
		 WHERE principal_id = ?`,
		int64(current.EditionInterval.Seconds()), current.EditionSize, unix(next), principalID)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no settings for %s", principalID))
}

// DueSettings returns everybody whose page is ready to be regenerated.
func (s *Store) DueSettings(ctx context.Context) ([]*Settings, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT s.principal_id, s.edition_interval, s.edition_size, s.next_edition_at
		   FROM settings s
		   JOIN principals p ON p.id = s.principal_id
		  WHERE s.next_edition_at <= ? AND p.disabled_at IS NULL
		  ORDER BY s.next_edition_at`,
		unix(s.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Settings
	for rows.Next() {
		var (
			set      Settings
			interval int64
			next     int64
		)
		if err := rows.Scan(&set.PrincipalID, &interval, &set.EditionSize, &next); err != nil {
			return nil, err
		}
		set.EditionInterval = time.Duration(interval) * time.Second
		set.NextEditionAt = time.Unix(next, 0).UTC()
		out = append(out, &set)
	}
	return out, rows.Err()
}

// ScheduleNextEdition moves a principal's clock forward.
func (s *Store) ScheduleNextEdition(ctx context.Context, principalID string, at time.Time) error {
	_, err := s.main.ExecContext(ctx,
		`UPDATE settings SET next_edition_at = ? WHERE principal_id = ?`, unix(at), principalID)
	return err
}

func validInterval(d time.Duration) bool {
	for _, valid := range EditionIntervals {
		if d == valid {
			return true
		}
	}
	return false
}

func validWindow(d time.Duration) bool {
	for _, valid := range ArticleWindows {
		if d == valid {
			return true
		}
	}
	return false
}

// EffectiveItemRetention is how long articles are kept, given what people have asked to
// see.
//
// Retention used to be a flat thirty days, which quietly made the longer windows a lie: a
// page set to show a year of articles would have had nothing older than a month to show.
// So the floor stays at thirty days and the ceiling at a year, and between them it follows
// whoever wants to see furthest back.
//
// "No limit" maps to the ceiling rather than to forever. Unbounded growth is not a setting
// anybody meant to choose, and a year is far past the point where a front page is about
// what is going on.
func (s *Store) EffectiveItemRetention(ctx context.Context) (time.Duration, error) {
	var longest int64
	err := s.main.QueryRowContext(ctx,
		// A zero anywhere means some feed was asked to reach back without limit, so it
		// takes the ceiling. Read from subscriptions, because that is where the window
		// lives: a feed nobody follows any more constrains nothing.
		`SELECT CASE WHEN count(*) = 0 THEN ?
		             WHEN min(max_article_age) = 0 THEN ?
		             ELSE max(max_article_age) END
		   FROM subscriptions`,
		int64(MinItemRetention.Seconds()), int64(MaxItemRetention.Seconds())).Scan(&longest)
	if err != nil {
		return MinItemRetention, err
	}

	retention := time.Duration(longest) * time.Second
	if retention < MinItemRetention {
		return MinItemRetention, nil
	}
	if retention > MaxItemRetention {
		return MaxItemRetention, nil
	}
	return retention, nil
}
