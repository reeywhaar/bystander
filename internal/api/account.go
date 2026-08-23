package api

import (
	"net/http"
	"strings"

	"bystander/internal/session"
)

// accountBody is somebody's own account, as only they see it.
//
// Separate from meBody, which every island fetches on load and which stays the small thing
// it is: a name, a role, and whether to show the admin link. What an account *is* — the
// address it could be recovered through, whether this instance can send anything to it —
// belongs to the one page that shows it.
type accountBody struct {
	Username      string `json:"username"`
	Role          string `json:"role"`
	CreatedAt     int64  `json:"created_at"`
	RecoveryEmail string `json:"recovery_email"`
	// MailConfigured is whether a relay exists at all.
	//
	// Here because a recovery address is worth nothing without one, and a page that
	// invited somebody to add an address while quietly being unable to send to it would
	// be making a promise the instance cannot keep. Not a secret: whether mail works is
	// something an account holder finds out the hard way otherwise.
	MailConfigured bool `json:"mail_configured"`
}

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	s.writeAccount(w, r, principalOf(r).ID)
}

// writeAccount renders an account, read fresh.
//
// By id rather than from the request's own principal, because that one was loaded when the
// session was resolved — before any of this request's writes. Rendering it back after a
// PATCH would answer with the values the request came in with, which looks exactly like a
// write that silently did nothing.
func (s *Server) writeAccount(w http.ResponseWriter, r *http.Request, id string) {
	p, err := s.store.PrincipalByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	relay, err := s.store.SMTPConfigured(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, accountBody{
		Username:       p.Username,
		Role:           string(p.Role),
		CreatedAt:      p.CreatedAt.Unix(),
		RecoveryEmail:  p.RecoveryEmail,
		MailConfigured: relay,
	})
}

type patchAccountRequest struct {
	RecoveryEmail *string `json:"recovery_email"`
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	var body patchAccountRequest
	if !decode(w, r, &body) {
		return
	}
	if body.RecoveryEmail == nil {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	id := principalOf(r).ID
	if err := s.store.SetRecoveryEmail(r.Context(), id, *body.RecoveryEmail); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeAccount(w, r, id)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword replaces somebody's own password, having checked they know the old one.
//
// Every other session ends and this one does not. "Changing my password signs out my other
// devices" is what people mean by it, and signing them out of the tab they are typing in
// would be a strange way to confirm it worked.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body changePasswordRequest
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.CurrentPassword) == "" {
		writeError(w, http.StatusBadRequest, "your current password is required")
		return
	}

	p := principalOf(r)
	// The same two buckets the login form has, for the same two reasons: bcrypt at cost 12
	// is a lever whoever pulls it, and this endpoint checks a password just as that one
	// does. A session cookie is not a reason to skip either — a borrowed cookie trying
	// passwords is exactly the case the per-account bucket is for.
	if !s.logins.allow("") || !s.logins.allow(strings.ToLower(p.Username)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait a minute")
		return
	}

	// The token this request arrived with, so the session it belongs to survives. Read
	// from the cookie rather than threaded down from the middleware: the middleware hands
	// on who you are, and this is the one handler that needs to know *which* session
	// said so.
	keep := ""
	if cookie, err := r.Cookie(session.CookieName); err == nil {
		keep = cookie.Value
	}

	if err := s.store.ChangePassword(r.Context(), p.ID, body.CurrentPassword, body.NewPassword, keep); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("password changed", "principal", p.ID, "username", p.Username)
	w.WriteHeader(http.StatusNoContent)
}
