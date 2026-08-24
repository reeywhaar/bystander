package api

import (
	"net/http"
	"time"

	"bystander/internal/store"
)

type settingsBody struct {
	EditionInterval int64 `json:"edition_interval"` // seconds
	EditionSize     int   `json:"edition_size"`
	NextEditionAt   int64 `json:"next_edition_at"`
}

// getSettings answers with the main page's cadence and size.
//
// These are a page's settings rather than a person's, and have been since pages became a list.
// This endpoint stays pointed at the main page so that everything which knew about one page
// keeps working while the interface catches up; the per-page controls will address a page by id.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	page, err := s.store.PageByID(r.Context(), store.MainPageID(principalOf(r).ID))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody{
		EditionInterval: int64(page.EditionInterval.Seconds()),
		EditionSize:     page.EditionSize,
		NextEditionAt:   page.NextEditionAt.Unix(),
	})
}

type patchSettingsRequest struct {
	EditionInterval *int64 `json:"edition_interval"`
	EditionSize     *int   `json:"edition_size"`
}

func (s *Server) patchSettings(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body patchSettingsRequest
	if !decode(w, r, &body) {
		return
	}

	patch := store.PagePatch{EditionSize: body.EditionSize}
	if body.EditionInterval != nil {
		d := time.Duration(*body.EditionInterval) * time.Second
		patch.EditionInterval = &d
	}
	if err := s.store.UpdatePage(r.Context(), store.MainPageID(p.ID), patch); err != nil {
		s.fail(w, r, err)
		return
	}
	s.getSettings(w, r)
}
