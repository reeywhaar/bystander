package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a live login.
//
// There is no field holding the cookie value, and there is no way to obtain one from this
// package. The table is keyed by sha256 of that value and the hash is all that is ever
// written down, so a database file, a backup, a heap dump or a swapped page never contains
// something replayable as a credential — and the lookup is timing-safe without trying to
// be, because telling two rows apart by timing would mean finding a 256-bit preimage.
type Session struct {
	PrincipalID string
	CreatedAt   time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
}

// CreateSession records a login. token is the cookie value; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, token, principalID string, expires time.Time) error {
	now := s.Now()
	_, err := s.main.ExecContext(ctx,
		`INSERT INTO sessions (id_hash, principal_id, created_at, last_seen_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), principalID, unix(now), unix(now), unix(expires))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionByToken returns a live session, or ErrNotFound.
//
// Expiry is applied in the query rather than after it. A lapsed row is not a session that
// exists and is refused; it is not a session, and the sweep will collect it.
func (s *Store) SessionByToken(ctx context.Context, token string) (*Session, error) {
	var (
		sess     Session
		created  int64
		lastSeen int64
		expires  int64
	)
	err := s.main.QueryRowContext(ctx,
		`SELECT principal_id, created_at, last_seen_at, expires_at
		   FROM sessions WHERE id_hash = ? AND expires_at > ?`,
		hashToken(token), unix(s.Now())).Scan(&sess.PrincipalID, &created, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no live session")
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	sess.ExpiresAt = time.Unix(expires, 0).UTC()
	return &sess, nil
}

// TouchSession slides a session's window forward.
//
// Called only when the session has not been touched for a while — see session.Refresh.
// Without that throttle, a polling interface would rewrite this row and emit a Set-Cookie
// on every single request, for a window measured in days.
func (s *Store) TouchSession(ctx context.Context, token string, expires time.Time) error {
	_, err := s.main.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id_hash = ?`,
		unix(s.Now()), unix(expires), hashToken(token))
	return err
}

// DeleteSession signs one session out. Deleting a session that is already gone is not an
// error: a logout is a statement about the future, not a claim about the present.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.main.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, hashToken(token))
	return err
}

// SweepSessions collects lapsed rows. They are already unusable — SessionByToken filters
// on expiry — so this only reclaims space.
func (s *Store) SweepSessions(ctx context.Context) (int64, error) {
	res, err := s.main.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, unix(s.Now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
