package api

import (
	"errors"
	"net/http"
	"strings"

	"bystander/internal/store"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type meBody struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if !decode(w, r, &body) {
		return
	}
	body.Username = strings.TrimSpace(body.Username)

	// Two buckets. The global one is the CPU limit — bcrypt at cost 12 is expensive on
	// purpose, and an unauthenticated endpoint that runs it is a lever. The per-name one
	// is the guessing limit, and it is keyed on the name so one account under attack does
	// not lock everybody else out.
	if !s.logins.allow("") || !s.logins.allow(strings.ToLower(body.Username)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait a minute")
		return
	}

	p, err := s.store.Authenticate(r.Context(), body.Username, body.Password)
	if err != nil {
		// Every refusal is a 401 with the store's sentence, whether the name was unknown,
		// the password wrong, or the account disabled. Telling them apart would turn the
		// login form into a list of who has an account here.
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		s.fail(w, r, err)
		return
	}

	if err := s.sessions.Issue(r.Context(), w, p.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("signed in", "principal", p.ID, "username", p.Username)
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Revoke(r.Context(), w, r); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	writeJSON(w, http.StatusOK, meBody{
		ID:        p.ID,
		Username:  p.Username,
		Role:      string(p.Role),
		CreatedAt: p.CreatedAt.Unix(),
	})
}

type inviteBody struct {
	Role      string `json:"role"`
	ExpiresAt int64  `json:"expires_at"`
	Usable    bool   `json:"usable"`
	Accepted  bool   `json:"accepted"`
	Expired   bool   `json:"expired"`
	// Email is the address this invitation was sent to, or empty for one handed over. Shown
	// to whoever holds the token — which, for an emailed one, is whoever can read that inbox
	// — so the page can say that it will become the account's recovery address. Somebody
	// should be told what an account of theirs is about to be attached to.
	Email string `json:"email"`
}

// invite reports what state a link is in, before somebody types a password into it.
//
// Valid, expired and already-accepted are three states a person can act on differently —
// wait for a new link, ask for one, or just sign in — and collapsing them into "no" would
// leave all three doing the same useless thing. Which state a token is in is not a secret
// from whoever is holding that token.
func (s *Server) invite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.InviteByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := s.store.Now()
	writeJSON(w, http.StatusOK, inviteBody{
		Role:      string(inv.Role),
		Email:     inv.Email,
		ExpiresAt: inv.ExpiresAt.Unix(),
		Usable:    inv.Usable(now),
		Accepted:  inv.Accepted(),
		Expired:   inv.Expired(now),
	})
}

// acceptInvite turns a link into an account and signs the new account in.
//
// Signing in immediately rather than sending them to the login form: they have just chosen
// a password, so asking them to type it again proves nothing and is one more place to lose
// somebody.
func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if !decode(w, r, &body) {
		return
	}
	// Shares the login bucket: this endpoint is unauthenticated and runs bcrypt, which is
	// the property that matters, not what it is called.
	if !s.logins.allow("") {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait a minute")
		return
	}

	p, displaced, err := s.store.AcceptInvite(r.Context(), r.PathValue("token"), body.Username, body.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// An emailed invitation's address is bound to the new account, and one address belongs to
	// one account — so this may have taken it off another. There is nowhere to tell them: the
	// only address on file for them is the one they just lost.
	if displaced != "" {
		s.log.Warn("a recovery address moved to a new account that was invited at it",
			"from", displaced, "to", p.ID)
	}
	if err := s.sessions.Issue(r.Context(), w, p.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("an invitation was accepted", "principal", p.ID, "username", p.Username, "role", p.Role)
	writeJSON(w, http.StatusNoContent, nil)
}
