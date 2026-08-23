// Package session turns a cookie into an account.
//
// Everything about how a session is stored — that the table is keyed by a hash, that the
// value itself is never written down — belongs to internal/store. What lives here is the
// part that touches HTTP: minting the value, setting and clearing the cookie, and the
// sliding window.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bystander/internal/store"
)

// CookieName is the session cookie.
const CookieName = "bystander_auth"

const (
	// TTL is the sliding window: a session dies this long after its last use. One week,
	// so somebody who reads their page on Sunday is still signed in the next Sunday.
	TTL = 7 * 24 * time.Hour

	// Refresh throttles the slide. Without it a polling interface rewrites the row and
	// emits a Set-Cookie on every request, for a window measured in days. An hour of
	// imprecision on a one-week window is not imprecision anybody can perceive.
	Refresh = time.Hour

	// SweepInterval is how often lapsed rows are collected. They are already unusable by
	// then; this reclaims the space.
	SweepInterval = 10 * time.Minute

	// tokenBytes is the entropy in a session id. 32 bytes is 256 bits.
	tokenBytes = 32
)

// Table issues and resolves sessions.
type Table struct {
	store  *store.Store
	secure bool
	log    *slog.Logger
	now    func() time.Time
}

// New builds a table. secure is whether the cookie carries the Secure attribute, which is
// the public URL being https and nothing else — see config.Config.
func New(st *store.Store, secure bool, log *slog.Logger) *Table {
	return &Table{store: st, secure: secure, log: log, now: time.Now}
}

// SetClock replaces the clock. For tests; the daemon never calls it.
func (t *Table) SetClock(now func() time.Time) { t.now = now }

// Issue records a login and sets the cookie.
func (t *Table) Issue(ctx context.Context, w http.ResponseWriter, principalID string) error {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	expires := t.now().UTC().Add(TTL)
	if err := t.store.CreateSession(ctx, token, principalID, expires); err != nil {
		return err
	}
	t.setCookie(w, token, expires)
	return nil
}

// Resolve returns the account a request is signed in as, or nil.
//
// A nil session and a nil error is the ordinary "not signed in" answer. An error means the
// database could not be asked, which is a different thing and deserves a 500 rather than a
// login page.
func (t *Table) Resolve(ctx context.Context, w http.ResponseWriter, r *http.Request) (*store.Principal, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, nil
	}

	sess, err := t.store.SessionByToken(ctx, cookie.Value)
	if err != nil {
		if isNotFound(err) {
			// The cookie names a session that has lapsed or been revoked. Clearing it
			// stops the browser presenting it on every subsequent request for a week.
			t.clearCookie(w)
			return nil, nil
		}
		return nil, err
	}

	p, err := t.store.PrincipalByID(ctx, sess.PrincipalID)
	if err != nil {
		if isNotFound(err) {
			t.clearCookie(w)
			return nil, nil
		}
		return nil, err
	}
	// A session outliving its account's suspension would make disabling an account mean
	// nothing until the cookie happened to lapse. store.SetDisabled deletes them, so this
	// is the belt to that braces — and it costs a field comparison already loaded.
	if p.Disabled() {
		t.clearCookie(w)
		return nil, nil
	}

	t.slide(ctx, w, cookie.Value, sess)
	return p, nil
}

// slide moves the window forward, at most once an hour per session.
func (t *Table) slide(ctx context.Context, w http.ResponseWriter, token string, sess *store.Session) {
	now := t.now().UTC()
	if now.Sub(sess.LastSeenAt) < Refresh {
		return
	}
	expires := now.Add(TTL)
	if err := t.store.TouchSession(ctx, token, expires); err != nil {
		// Not fatal, and not worth failing a request over: the session is still valid
		// until its current expiry, and the next request will try again.
		t.log.Warn("could not slide a session forward", "error", err)
		return
	}
	t.setCookie(w, token, expires)
}

// Revoke signs the request's session out and clears the cookie.
func (t *Table) Revoke(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	t.clearCookie(w)
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	return t.store.DeleteSession(ctx, cookie.Value)
}

// Run sweeps lapsed sessions until ctx is cancelled.
func (t *Table) Run(ctx context.Context) {
	ticker := time.NewTicker(SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := t.store.SweepSessions(ctx)
			if err != nil && ctx.Err() == nil {
				t.log.Warn("could not sweep sessions", "error", err)
			}
			if n > 0 {
				t.log.Debug("swept lapsed sessions", "count", n)
			}
		}
	}
}

func (t *Table) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:  CookieName,
		Value: token,
		Path:  "/",
		// HttpOnly because no script has any business reading this: the only thing that
		// needs it is the browser, on its way out.
		HttpOnly: true,
		// Lax rather than Strict: an invitation link arriving from a chat app is a
		// cross-site navigation, and Strict would mean landing on it signed out.
		SameSite: http.SameSiteLaxMode,
		Secure:   t.secure,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func (t *Table) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   t.secure,
		MaxAge:   -1,
	})
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
