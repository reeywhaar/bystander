package api

import (
	"context"
	"net/http"

	mailer "bystander/internal/mail"
	"bystander/internal/store"
)

type userBody struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	CreatedAt  int64  `json:"created_at"`
	DisabledAt *int64 `json:"disabled_at"`
	FeedCount  int    `json:"feed_count"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	principals, err := s.store.ListPrincipals(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	out := make([]userBody, 0, len(principals))
	for _, p := range principals {
		body := userBody{
			ID:        p.ID,
			Username:  p.Username,
			Role:      string(p.Role),
			CreatedAt: p.CreatedAt.Unix(),
		}
		if p.Disabled() {
			at := p.DisabledAt.Unix()
			body.DisabledAt = &at
		}
		// A count rather than the list: an administrator wants to know whether an account
		// is in use, not what it reads.
		if subs, err := s.store.ListSubscriptions(r.Context(), p.ID); err == nil {
			body.FeedCount = len(subs)
		}
		out = append(out, body)
	}
	writeJSON(w, http.StatusOK, out)
}

type patchUserRequest struct {
	Disabled *bool `json:"disabled"`
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	caller := principalOf(r)
	id := r.PathValue("id")

	var body patchUserRequest
	if !decode(w, r, &body) {
		return
	}
	if body.Disabled == nil {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	if *body.Disabled {
		if id == caller.ID {
			writeError(w, http.StatusConflict, "you cannot disable your own account")
			return
		}
		if err := s.refuseIfLastAdmin(r, id); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}

	if err := s.store.SetDisabled(r.Context(), id, *body.Disabled); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("an account was changed", "by", caller.ID, "principal", id, "disabled", *body.Disabled)
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	caller := principalOf(r)
	id := r.PathValue("id")

	if id == caller.ID {
		writeError(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if err := s.refuseIfLastAdmin(r, id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.store.DeletePrincipal(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("an account was deleted", "by", caller.ID, "principal", id)
	writeJSON(w, http.StatusNoContent, nil)
}

// refuseIfLastAdmin stops the change that would lock everybody out.
//
// There is no recovery path that does not involve a shell on the host, so this is worth a
// query on a rare operation.
func (s *Server) refuseIfLastAdmin(r *http.Request, id string) error {
	target, err := s.store.PrincipalByID(r.Context(), id)
	if err != nil {
		return err
	}
	if target.Role != store.RoleAdmin || target.Disabled() {
		return nil
	}
	n, err := s.store.CountAdmins(r.Context())
	if err != nil {
		return err
	}
	if n <= 1 {
		return errLastAdmin
	}
	return nil
}

var errLastAdmin = &lastAdminError{}

type lastAdminError struct{}

func (*lastAdminError) Error() string {
	return "that is the last administrator; make somebody else one first"
}

type adminInviteBody struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	// Email is the address it was sent to, or empty for one minted to be handed over.
	Email      string `json:"email"`
	AcceptedAt *int64 `json:"accepted_at"`
	Username   string `json:"username"`
	// URL is present only for an invitation minted to be handed over, and only in the reply
	// that minted it. An emailed one never carries it — see createInvite.
	URL *string `json:"url,omitempty"`
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.store.ListInvites(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	out := make([]adminInviteBody, 0, len(invites))
	for _, inv := range invites {
		body := adminInviteBody{
			ID:        inv.ID,
			Role:      string(inv.Role),
			CreatedAt: inv.CreatedAt.Unix(),
			ExpiresAt: inv.ExpiresAt.Unix(),
			Email:     inv.Email,
		}
		if inv.Accepted() {
			at := inv.AcceptedAt.Unix()
			body.AcceptedAt = &at
			// Who it became, which is the only reason to keep looking at an accepted
			// invitation.
			if p, err := s.store.PrincipalByID(r.Context(), inv.PrincipalID); err == nil {
				body.Username = p.Username
			}
		}
		out = append(out, body)
	}
	writeJSON(w, http.StatusOK, out)
}

type createInviteRequest struct {
	Role string `json:"role"`
	// Email sends the link there instead of handing it back. Empty mints a link to pass on.
	Email string `json:"email"`
}

// createInvite mints a link, and either sends it or hands it over.
//
// # Handed over
//
// The reply carries the full URL, and it is the only time the token is ever readable: what is
// stored is a hash, so a lost link is reissued rather than recovered. Same stance as sessions,
// same reason.
//
// # Sent
//
// The reply carries no URL at all, and that is the feature rather than an omission. Accepting
// an emailed invitation is what binds that address to the new account as a *proved* recovery
// address, and the proof is that the link went to that inbox and nowhere else. Hand the same
// link to the administrator as well and the proof is gone: whoever accepted it need never have
// read the address it is now bound to.
//
// So the two are exclusive by construction. An administrator who wants to pass a link along
// themselves mints one without an address; one who has an address sends it there. A mail that
// bounces is a new invitation, not a link to fall back on.
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	caller := principalOf(r)

	var body createInviteRequest
	if !decode(w, r, &body) {
		return
	}
	role := store.Role(body.Role)
	if body.Role == "" {
		role = store.RoleUser
	}

	// Everything that could refuse is asked before anything is written, so a refusal never
	// leaves an invitation nobody will receive sitting in the table.
	var relay *mailer.Settings
	if body.Email != "" {
		found, err := s.store.SMTPSettings(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if found == nil {
			writeError(w, http.StatusConflict,
				"no mail relay is configured on this instance, so an invitation cannot be sent")
			return
		}
		relay = found

		// This posts mail to an address the caller chose. Per account rather than global: an
		// administrator inviting a team is normal, and a thousand is a relay being used to
		// send somebody else's post.
		if !s.mail.allow(caller.ID) {
			writeError(w, http.StatusTooManyRequests, "that is a lot of invitations; wait a minute")
			return
		}
	}

	inv, token, err := s.store.CreateInvite(r.Context(), role, caller.ID, body.Email)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	link := s.cfg.Link("/invite/" + token)

	out := adminInviteBody{
		ID:        inv.ID,
		Role:      string(inv.Role),
		CreatedAt: inv.CreatedAt.Unix(),
		ExpiresAt: inv.ExpiresAt.Unix(),
		Email:     inv.Email,
	}

	if relay == nil {
		out.URL = &link
		s.log.Info("an invitation was created", "by", caller.ID, "invite", inv.ID, "role", role)
		writeJSON(w, http.StatusCreated, out)
		return
	}

	if err := s.sendMail(r.Context(), *relay, mailer.Message{
		To:      inv.Email,
		Subject: "You have been invited to bystander",
		Body: "Somebody has invited you to a bystander instance — an RSS reader with no unread count.\n\n" +
			link + "\n\n" +
			"Open it to choose a name and a password. The link is good until " +
			inv.ExpiresAt.Format("2 January 2006") + ", and works once.\n\n" +
			"This address will become the recovery address for the account, because this " +
			"invitation reached you at it.\n\n" +
			"If you were not expecting this, nothing has been created and you can ignore it.\n",
	}); err != nil {
		// Nothing was sent, so nothing should be outstanding. Left in place, the invitation
		// would sit in the list looking issued while its only copy of the link is gone — and
		// the way out of that state is minting another one anyway.
		//
		// WithoutCancel because this must happen even when the request is being abandoned:
		// the row is the thing that would otherwise outlive the failure.
		if err := s.store.DeleteInvite(context.WithoutCancel(r.Context()), inv.ID); err != nil {
			s.log.Error("could not withdraw an invitation that was never sent",
				"invite", inv.ID, "err", err)
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// The address, never the link. A log is read by more people than the one who should have it.
	s.log.Info("an invitation was sent", "by", caller.ID, "invite", inv.ID,
		"role", role, "email", inv.Email)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteInvite(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
