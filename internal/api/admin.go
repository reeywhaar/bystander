package api

import (
	"net/http"

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
	ID         string  `json:"id"`
	Role       string  `json:"role"`
	CreatedAt  int64   `json:"created_at"`
	ExpiresAt  int64   `json:"expires_at"`
	AcceptedAt *int64  `json:"accepted_at"`
	Username   string  `json:"username"`
	URL        *string `json:"url,omitempty"`
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
}

// createInvite mints a link.
//
// The response carries the full URL, and it is the only time the token is ever readable:
// what is stored is a hash, so a lost link is reissued rather than recovered. Same stance
// as sessions, same reason.
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

	inv, token, err := s.store.CreateInvite(r.Context(), role, caller.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	link := s.cfg.Link("/invite/" + token)

	s.log.Info("an invitation was created", "by", caller.ID, "invite", inv.ID, "role", role)
	writeJSON(w, http.StatusCreated, adminInviteBody{
		ID:        inv.ID,
		Role:      string(inv.Role),
		CreatedAt: inv.CreatedAt.Unix(),
		ExpiresAt: inv.ExpiresAt.Unix(),
		URL:       &link,
	})
}

func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteInvite(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
