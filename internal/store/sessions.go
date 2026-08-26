package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"bystander/internal/ids"
)

// Session is a live login.
//
// There is no field holding the cookie value, and there is no way to obtain one from this
// package. The table is keyed by sha256 of that value and the hash is all that is ever
// written down, so a database file, a backup, a heap dump or a swapped page never contains
// something replayable as a credential — and the lookup is timing-safe without trying to
// be, because telling two rows apart by timing would mean finding a 256-bit preimage.
type Session struct {
	// ID is what a session is called when it is being talked about rather than
	// presented — in a list of your own sign-ins, in the URL that revokes one. It is
	// derived from the stored hash, so it is stable for the life of the session and
	// reveals nothing: it is a hash of a hash of a token nobody but the browser holds.
	ID          string
	PrincipalID string
	CreatedAt   time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
	Device
}

// Device is what little a request says about where it came from.
//
// Neither field is evidence of anything. An address is whatever the proxy in front of us
// resolved it to and belongs to a network rather than a person; a user agent is a string
// the browser chose to send, and browsers have been lying in it since 1993. They are here
// because they are enough to recognise your own sessions and disown one that is not yours,
// which is the only question this answers.
type Device struct {
	IP        string
	UserAgent string
}

// CreateSession records a login. token is the cookie value; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, token, principalID string, expires time.Time, dev Device) error {
	now := s.Now()
	_, err := s.main.ExecContext(ctx,
		`INSERT INTO sessions (id_hash, principal_id, created_at, last_seen_at, expires_at, last_ip, last_user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hashToken(token), principalID, unix(now), unix(now), unix(expires), dev.IP, dev.UserAgent)
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
		`SELECT principal_id, created_at, last_seen_at, expires_at, last_ip, last_user_agent
		   FROM sessions WHERE id_hash = ? AND expires_at > ?`,
		hashToken(token), unix(s.Now())).
		Scan(&sess.PrincipalID, &created, &lastSeen, &expires, &sess.IP, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no live session")
	}
	if err != nil {
		return nil, err
	}
	sess.ID = sessionID(hashToken(token))
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
func (s *Store) TouchSession(ctx context.Context, token string, expires time.Time, dev Device) error {
	_, err := s.main.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ?, last_ip = ?, last_user_agent = ?
		 WHERE id_hash = ?`,
		unix(s.Now()), unix(expires), dev.IP, dev.UserAgent, hashToken(token))
	return err
}

// Sessions lists one account's live sign-ins, most recently used first.
//
// Lapsed rows are filtered rather than swept: the sweep runs on its own clock and a session
// that expired four minutes ago should not still be offered for revoking.
func (s *Store) Sessions(ctx context.Context, principalID string) ([]*Session, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT id_hash, created_at, last_seen_at, expires_at, last_ip, last_user_agent
		   FROM sessions
		  WHERE principal_id = ? AND expires_at > ?
		  ORDER BY last_seen_at DESC`,
		principalID, unix(s.Now()))
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		var (
			idHash                     []byte
			created, lastSeen, expires int64
			sess                       = Session{PrincipalID: principalID}
		)
		if err := rows.Scan(&idHash, &created, &lastSeen, &expires, &sess.IP, &sess.UserAgent); err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		sess.ID = sessionID(idHash)
		sess.CreatedAt = time.Unix(created, 0).UTC()
		sess.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		sess.ExpiresAt = time.Unix(expires, 0).UTC()
		out = append(out, &sess)
	}
	return out, rows.Err()
}

// RevokeSession signs out one of an account's sessions by its public id.
//
// The id is a digest, so there is no query that turns it back into a key: the account's own
// rows are read and matched. That is a handful of rows and it is the scoping too — an id
// belonging to somebody else's session matches nothing here rather than deleting it.
// Returns ErrNotFound if no session of this account carries that id.
func (s *Store) RevokeSession(ctx context.Context, principalID, id string) error {
	rows, err := s.main.QueryContext(ctx,
		`SELECT id_hash FROM sessions WHERE principal_id = ?`, principalID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	defer rows.Close()

	var target []byte
	for rows.Next() {
		var idHash []byte
		if err := rows.Scan(&idHash); err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
		if sessionID(idHash) == id {
			target = idHash
			break
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	rows.Close()
	if target == nil {
		return ErrNotFound
	}

	if _, err := s.main.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, target); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// RevokeOtherSessions signs out every session of an account except the one presenting
// token, and reports how many went. The caller keeps its cookie and everybody else is
// signed out on their next request — which is the point of the button that calls this.
func (s *Store) RevokeOtherSessions(ctx context.Context, principalID, token string) (int64, error) {
	res, err := s.main.ExecContext(ctx,
		`DELETE FROM sessions WHERE principal_id = ? AND id_hash <> ?`,
		principalID, hashToken(token))
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	return res.RowsAffected()
}

// SessionID is what the session presenting this token is called in public. For the caller
// holding a request: it can say which row in a list of sessions is the one reading it,
// without the token going anywhere or a lookup happening.
func SessionID(token string) string { return sessionID(hashToken(token)) }

// sessionID names a session in public. Deriving it from the stored hash rather than storing
// a second column means there is nothing to keep in step, and the hash itself — which is
// the credential's only remaining shadow — still never leaves this package.
func sessionID(idHash []byte) string { return ids.Derive(ids.Session, string(idHash)) }

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
