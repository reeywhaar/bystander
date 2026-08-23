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

// Settings is one person's edition preferences.
type Settings struct {
	PrincipalID     string
	EditionInterval time.Duration
	EditionSize     int
	NextEditionAt   time.Time
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
		`SELECT edition_interval, edition_size, next_edition_at FROM settings WHERE principal_id = ?`,
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

// UpdateSettings changes what was passed and leaves the rest alone. A nil pointer means
// "not mentioned", which is what a PATCH body means.
func (s *Store) UpdateSettings(ctx context.Context, principalID string, interval *time.Duration, size *int) error {
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
		`UPDATE settings SET edition_interval = ?, edition_size = ?, next_edition_at = ? WHERE principal_id = ?`,
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
