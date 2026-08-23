package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	mailer "bystander/internal/mail"
	"bystander/internal/session"
	"bystander/internal/store"
)

// accountBody is somebody's own account, as only they see it.
//
// Separate from meBody, which every island fetches on load and which stays the small thing
// it is: a name, a role, and whether to show the admin link. What an account *is* — the
// address it could be recovered through, whether this instance can send anything to it —
// belongs to the one page that shows it.
type accountBody struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
	// RecoveryEmail is the address this account has *proved* it can read, or empty.
	RecoveryEmail string `json:"recovery_email"`
	// RecoveryPending is an address partway through being proved, or empty.
	//
	// So a page reopened mid-flow says which address it is waiting on rather than starting
	// somebody over on a code they are already holding.
	RecoveryPending string `json:"recovery_pending"`
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
	proved, err := s.store.RecoveryEmail(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pending, err := s.store.PendingRecovery(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, accountBody{
		Username:        p.Username,
		Role:            string(p.Role),
		CreatedAt:       p.CreatedAt.Unix(),
		RecoveryEmail:   proved,
		RecoveryPending: pending,
		MailConfigured:  relay,
	})
}

type beginRecoveryRequest struct {
	Email string `json:"email"`
}

// beginRecovery sends a code to an address, and records nothing about the account.
//
// The account has no recovery address until the code comes back — not a provisional one.
// An address nobody has proved they can read is worse than none: a typo sends recovery to a
// stranger's inbox, and the owner finds out at the one moment they cannot afford to. And a
// borrowed session could otherwise point recovery at an address of its own and come back
// for the account later.
func (s *Server) beginRecovery(w http.ResponseWriter, r *http.Request) {
	var body beginRecoveryRequest
	if !decode(w, r, &body) {
		return
	}

	// Checked before anything is written, so nobody is left holding a code for a flow that
	// could never have finished.
	relay, err := s.store.SMTPSettings(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if relay == nil {
		writeError(w, http.StatusConflict,
			"no mail relay is configured on this instance, so a code cannot be sent")
		return
	}

	p := principalOf(r)
	// This sends mail to an address the caller chose. The ceiling is per account rather
	// than global: one person restarting a flow is normal, and a hundred is a relay being
	// used to post somebody else's mail.
	if !s.mail.allow(p.ID) {
		writeError(w, http.StatusTooManyRequests, "that is a lot of codes; wait a minute")
		return
	}

	code, err := s.store.BeginRecovery(r.Context(), p.ID, body.Email)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	address, err := s.store.PendingRecovery(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	if err := s.sendMail(r.Context(), *relay, mailer.Message{
		To:      address,
		Subject: "Confirm this address for your bystander account",
		Body: "Your confirmation code is " + code + "\n\n" +
			"It is good for " + strconv.Itoa(int(store.CodeTTL.Minutes())) + " minutes. " +
			"If you did not ask for this, nothing has changed and you can ignore it.\n",
	}); err != nil {
		// Nothing was sent, so nothing is waiting. Left in place, the row would have the
		// account page say "waiting on a code sent to …" about a code that never left —
		// and the way out of that state is the button somebody just watched fail.
		//
		// WithoutCancel because this must happen even when the request is being abandoned:
		// the row is the thing that would otherwise outlive the failure.
		if err := s.store.ClearPendingRecovery(context.WithoutCancel(r.Context()), p.ID); err != nil {
			s.log.Error("could not undo a recovery attempt that was never sent",
				"principal", p.ID, "err", err)
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// The address, never the code. A log is read by more people than the one who should
	// have it.
	s.log.Info("recovery code sent", "principal", p.ID, "email", address)
	w.WriteHeader(http.StatusNoContent)
}

type confirmRecoveryRequest struct {
	Code string `json:"code"`
}

// confirmRecovery is the only step that changes anything.
func (s *Server) confirmRecovery(w http.ResponseWriter, r *http.Request) {
	var body confirmRecoveryRequest
	if !decode(w, r, &body) {
		return
	}

	p := principalOf(r)
	// A short code guessed at from a signed-in session is still guessing. The store bounds
	// attempts per code; this bounds them per minute, so five codes a minute is the most
	// anybody gets to work through.
	if !s.logins.allow(strings.ToLower(p.Username)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait a minute")
		return
	}

	email, displaced, err := s.store.ConfirmRecovery(r.Context(), p.ID, body.Code)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if displaced != "" {
		// The only record that it happened. The displaced account cannot be told — the
		// only address on file for them is the one they have just lost.
		s.log.Warn("recovery address taken over", "from", displaced, "to", p.ID, "email", email)
	}
	s.log.Info("recovery address confirmed", "principal", p.ID, "email", email)
	s.writeAccount(w, r, p.ID)
}

// clearRecovery forgets the address and anything in flight.
func (s *Server) clearRecovery(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearRecovery(r.Context(), principalOf(r).ID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
