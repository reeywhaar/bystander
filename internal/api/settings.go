package api

import (
	"net/http"
	"time"

	"bystander/internal/store"
)

type settingsBody struct {
	EditionInterval int64 `json:"edition_interval"` // seconds
	EditionSize     int   `json:"edition_size"`
	// ArticleWindow is how old an article may be and still reach a page, in seconds.
	// Zero is no limit — bounded in practice by how long articles are kept.
	ArticleWindow int64 `json:"article_window"`
	NextEditionAt int64 `json:"next_edition_at"`
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
		ArticleWindow:   int64(settings.ArticleWindow.Seconds()),
		NextEditionAt:   settings.NextEditionAt.Unix(),
	})
}

type patchSettingsRequest struct {
	EditionInterval *int64 `json:"edition_interval"`
	EditionSize     *int   `json:"edition_size"`
	ArticleWindow   *int64 `json:"article_window"`
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
	if body.ArticleWindow != nil {
		d := time.Duration(*body.ArticleWindow) * time.Second
		patch.ArticleWindow = &d
	}
	if err := s.store.UpdateSettings(r.Context(), p.ID, patch); err != nil {
		s.fail(w, r, err)
		return
	}
	s.getSettings(w, r)
}
