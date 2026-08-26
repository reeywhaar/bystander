package api

import (
	"net/http"

	"bystander/internal/session"
	"bystander/internal/store"
)

// sessionBody is one sign-in, as the person who made it sees it.
//
// Everything here is descriptive. There is no field that anything decides on, which is what
// makes it acceptable to show an address a proxy reported and a name a browser chose for
// itself: the question this page answers is "do I recognise this", and a person is a better
// judge of that than any check would be.
type sessionBody struct {
	ID string `json:"id"`
	// Current marks the session reading this. It is the one the "sign out everywhere else"
	// button keeps, and the one whose revoke button is not offered.
	Current bool `json:"current"`
	// CreatedAt is when this sign-in happened. Together with LastAccess it separates a
	// session that has been quietly alive for a week from one that started an hour ago.
	CreatedAt int64 `json:"created_at"`
	// LastAccess is the last time this session was seen, to within an hour — see
	// session.Refresh, which exists so that a polling interface does not rewrite the row
	// on every request. An hour of vagueness on a window measured in days.
	LastAccess int64 `json:"last_access"`
	// ExpiresAt is when it lapses on its own if nothing uses it again.
	ExpiresAt int64 `json:"expires_at"`
	// IP is where it was last used from, or empty for a session that predates this being
	// recorded.
	IP string `json:"ip"`
	// UserAgent is the browser's own description of itself, verbatim. Shown under the
	// summary rather than instead of it, because the summary is a guess and this is what
	// the guess was made from.
	UserAgent string `json:"user_agent"`
	// Device is that guess: a browser and a platform, or empty when the string is not one
	// this recognises.
	Device string `json:"device"`
}

// listSessions lists this account's live sign-ins.
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Sessions(r.Context(), principalOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	current := currentSessionID(r)
	out := make([]sessionBody, 0, len(rows))
	for _, sess := range rows {
		out = append(out, sessionBody{
			ID:         sess.ID,
			Current:    sess.ID == current,
			CreatedAt:  sess.CreatedAt.Unix(),
			LastAccess: sess.LastSeenAt.Unix(),
			ExpiresAt:  sess.ExpiresAt.Unix(),
			IP:         sess.IP,
			UserAgent:  sess.UserAgent,
			Device:     describeAgent(sess.UserAgent),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeSession signs one of this account's sessions out.
//
// Including, deliberately, the current one: revoking the session you are reading from is a
// coherent thing to ask for, and the interface answers it by landing you on the login page.
// Refusing would only mean the button that does it has to be called something else.
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	id := r.PathValue("id")

	if err := s.store.RevokeSession(r.Context(), p.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	if id == currentSessionID(r) {
		// The cookie names a row that no longer exists. Resolve would clear it on the next
		// request anyway; doing it here means the redirect that follows is already signed
		// out rather than signed out one request later.
		if err := s.sessions.Revoke(r.Context(), w, r); err != nil {
			s.log.Warn("could not clear a revoked session's cookie", "principal", p.ID, "err", err)
		}
	}
	s.log.Info("session revoked", "principal", p.ID, "session", id)
	w.WriteHeader(http.StatusNoContent)
}

// revokeOtherSessions ends every sign-in but this one.
//
// The one exception is the session pressing the button. "Sign out everywhere else" that
// also signed you out of the tab you pressed it in would be a strange way to confirm it
// worked — the same reasoning as changing a password, which keeps this session for the
// same reason.
func (s *Server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	token := ""
	if cookie, err := r.Cookie(session.CookieName); err == nil {
		token = cookie.Value
	}

	n, err := s.store.RevokeOtherSessions(r.Context(), p.ID, token)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("other sessions revoked", "principal", p.ID, "count", n)
	w.WriteHeader(http.StatusNoContent)
}

// currentSessionID names the session this request arrived on, or "".
//
// Read from the cookie rather than threaded down from the middleware, which hands on who
// you are and not which of your sessions said so — the same reason changePassword reads it.
func currentSessionID(r *http.Request) string {
	cookie, err := r.Cookie(session.CookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return store.SessionID(cookie.Value)
}
