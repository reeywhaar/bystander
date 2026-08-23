package api

import (
	"net/http"

	"bystander/internal/store"
)

type tagBody struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ParentID  *string `json:"parent_id"`
	Priority  int     `json:"priority"`
	CreatedAt int64   `json:"created_at"`
}

func tagOf(tag *store.Tag) tagBody {
	body := tagBody{
		ID:        tag.ID,
		Name:      tag.Name,
		Priority:  tag.Priority,
		CreatedAt: tag.CreatedAt.Unix(),
	}
	if tag.ParentID != "" {
		parent := tag.ParentID
		body.ParentID = &parent
	}
	return body
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context(), principalOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]tagBody, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tagOf(tag))
	}
	writeJSON(w, http.StatusOK, out)
}

type addTagRequest struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
	Priority *int   `json:"priority"`
}

func (s *Server) addTag(w http.ResponseWriter, r *http.Request) {
	var body addTagRequest
	if !decode(w, r, &body) {
		return
	}
	priority := store.DefaultPriority
	if body.Priority != nil {
		priority = *body.Priority
	}

	tag, err := s.store.CreateTag(r.Context(), principalOf(r).ID, body.Name, body.ParentID, priority)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tagOf(tag))
}

type patchTagRequest struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
	Priority *int    `json:"priority"`
}

func (s *Server) patchTag(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body patchTagRequest
	if !decode(w, r, &body) {
		return
	}
	if err := s.store.UpdateTag(r.Context(), p.ID, r.PathValue("id"), body.Name, body.ParentID, body.Priority); err != nil {
		s.fail(w, r, err)
		return
	}
	tag, err := s.store.TagByID(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tagOf(tag))
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTag(r.Context(), principalOf(r).ID, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
