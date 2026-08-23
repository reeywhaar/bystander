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

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings(r.Context(), principalOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody{
		EditionInterval: int64(settings.EditionInterval.Seconds()),
		EditionSize:     settings.EditionSize,
		NextEditionAt:   settings.NextEditionAt.Unix(),
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

	patch := store.SettingsPatch{EditionSize: body.EditionSize}
	if body.EditionInterval != nil {
		d := time.Duration(*body.EditionInterval) * time.Second
		patch.EditionInterval = &d
	}
	if err := s.store.UpdateSettings(r.Context(), p.ID, patch); err != nil {
		s.fail(w, r, err)
		return
	}
	s.getSettings(w, r)
}
