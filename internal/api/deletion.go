package api

import (
	"net/http"
	"strings"
	"time"

	"bystander/internal/app"

	mailer "bystander/internal/mail"
	"bystander/internal/store"
)

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// deletionBody is the answer to asking to be erased.
type deletionBody struct {
	// DeletedAt is when the request was made, and PurgeAt when it takes effect.
	DeletedAt int64 `json:"deleted_at"`
	PurgeAt   int64 `json:"purge_at"`
	// Notified is whether a message went to the recovery address.
	//
	// Reported rather than assumed, because the message is the safety net for the case this
	// endpoint most needs one — somebody else pressing this through a session they should
	// not have — and an account with no address on file has no safety net. The interface
	// says which of the two happened instead of implying the better one.
	Notified bool `json:"notified"`
}

// scheduleDeletion asks for an account to be erased, a week from now.
//
// Not erased here. A week, and any sign-in during it calls the whole thing off — see
// store.ScheduleDeletion, and the sign-in handler, where the withdrawal happens. The delay
// is not politeness: "delete my account" pressed by mistake, or pressed by somebody who
// should not have the session, is a thing that has to be recoverable, and the only recovery
// that works for a person who has just lost their password *and* their account is one they
// do not have to ask anybody for.
//
// The password is required for the same reason changing one requires it: being signed in is
// not the same as knowing it.
func (s *Server) scheduleDeletion(w http.ResponseWriter, r *http.Request) {
	var body deleteAccountRequest
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Password) == "" {
		writeError(w, http.StatusBadRequest, "your password is required")
		return
	}

	p := principalOf(r)
	// The same two buckets the login form has. A session cookie is not a reason to skip
	// either — a borrowed cookie working through passwords is exactly the case the
	// per-account bucket is for, and bcrypt at cost 12 is a lever whoever pulls it.
	if !s.logins.allow("") || !s.logins.allow(strings.ToLower(p.Username)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait a minute")
		return
	}

	// The one account that cannot go. There is no recovery path from an instance with no
	// administrator that does not involve a shell on the host, and somebody deleting
	// themselves is a likelier way to arrive there than an administrator deleting the last
	// of their colleagues.
	if err := s.refuseIfLastAdmin(r, p.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	deleted, err := s.store.ScheduleDeletion(r.Context(), p.ID, body.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	purge := deleted.Add(store.DeletionGrace)

	// After the deletion is recorded rather than before. A message announcing something
	// that then failed to happen is worse than no message, and this one's whole job is to
	// be true.
	notified := s.sayGoodbye(r, p, purge)

	// Every session ended with the request, including this one, so the cookie names a
	// session that no longer exists. Clearing it here rather than leaving Resolve to do it
	// on the next request means the interface is signed out by the time it navigates.
	if err := s.sessions.Revoke(r.Context(), w, r); err != nil {
		s.log.Warn("could not clear the cookie of an account that asked to be erased",
			"principal", p.ID, "err", err)
	}

	s.log.Info("an account asked to be erased",
		"principal", p.ID, "username", p.Username, "purge_at", purge, "notified", notified)
	writeJSON(w, http.StatusOK, deletionBody{
		DeletedAt: deleted.Unix(),
		PurgeAt:   purge.Unix(),
		Notified:  notified,
	})
}

// sayGoodbye writes to the recovery address, and reports whether it went.
//
// Never fatal. The request has already been recorded, and refusing it because a relay was
// unreachable would mean an instance with a broken relay is an instance nobody can leave.
// What the message is *for* is the case where the person reading it did not ask: it names
// the date and says the one thing that undoes it, which is something they can do without
// anybody's help.
func (s *Server) sayGoodbye(r *http.Request, p *store.Principal, purge time.Time) bool {
	relay, err := s.store.SMTPSettings(r.Context())
	if err != nil || relay == nil {
		return false
	}
	address, err := s.store.RecoveryEmail(r.Context(), p.ID)
	if err != nil || address == "" {
		return false
	}

	when := purge.Format("2 January 2006")
	if err := s.sendMail(r.Context(), *relay, mailer.Message{
		To:      address,
		Subject: "Your " + app.Name + " account will be erased on " + when,
		Body: "Somebody asked for the " + app.Name + " account " + p.Username +
			", at " + s.cfg.PublicURL.String() + ", to be deleted.\n\n" +
			"Nothing has been erased yet. On " + when + " the account and everything in it " +
			"— every feed, tag, front page and the record of what you have read — will be " +
			"removed, and that cannot be undone.\n\n" +
			"If this was you, there is nothing to do.\n\n" +
			"If it was not, sign in at " + s.cfg.PublicURL.String() + " before " + when +
			". Signing in cancels the deletion by itself; there is no button to find. " +
			"Change your password once you are back in, because somebody was able to do " +
			"this from a session of yours.\n",
	}); err != nil {
		// Logged rather than returned. Whether a relay accepted a message is not something
		// the person who pressed the button can do anything about.
		s.log.Error("could not write to an account that asked to be erased",
			"principal", p.ID, "err", err)
		return false
	}
	s.log.Info("wrote to an account that asked to be erased", "principal", p.ID, "email", address)
	return true
}
