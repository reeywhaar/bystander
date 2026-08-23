package api

import (
	"errors"
	"net/http"

	"bystander/internal/store"
)

type editionBody struct {
	ID            string        `json:"id"`
	GeneratedAt   int64         `json:"generated_at"`
	NextEditionAt int64         `json:"next_edition_at"`
	Size          int           `json:"size"`
	Items         []articleBody `json:"items"`
}

// articleBody is one card.
//
// The feed is denormalised onto each article rather than sent as a side table: a card
// shows its source, the client would join every one of them anyway, and a page is sixty
// rows.
type articleBody struct {
	ID          string   `json:"id"`
	Rank        int      `json:"rank"`
	Slot        string   `json:"slot"`
	ReadAt      *int64   `json:"read_at"`
	Title       string   `json:"title"`
	Link        string   `json:"link"`
	Author      string   `json:"author"`
	Summary     string   `json:"summary"`
	ImageURL    string   `json:"image_url"`
	PublishedAt int64    `json:"published_at"`
	Feed        feedStub `json:"feed"`
}

type feedStub struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	SiteURL string `json:"site_url"`
}

func (s *Server) edition(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	settings, err := s.store.Settings(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	ed, items, err := s.store.CurrentEdition(r.Context(), p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Not a 404. "Your page has not been generated yet" is a state the reader
			// renders, not an error it reports — and a 404 on the one endpoint the front
			// page calls would send the interface into its failure path on a new
			// account's very first visit.
			writeJSON(w, http.StatusOK, editionBody{
				NextEditionAt: settings.NextEditionAt.Unix(),
				Size:          settings.EditionSize,
				Items:         []articleBody{},
			})
			return
		}
		s.fail(w, r, err)
		return
	}

	// Titles come from the subscriptions, so a person's own name for a feed is what
	// appears on their page.
	subs, err := s.store.ListSubscriptions(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	titles := make(map[string]feedStub, len(subs))
	for _, sub := range subs {
		titles[sub.FeedID] = feedStub{ID: sub.FeedID, Title: sub.Title(), SiteURL: sub.Feed.SiteURL}
	}

	body := editionBody{
		ID:            ed.ID,
		GeneratedAt:   ed.GeneratedAt.Unix(),
		NextEditionAt: settings.NextEditionAt.Unix(),
		Size:          ed.Size,
		Items:         make([]articleBody, 0, len(items)),
	}
	for _, entry := range items {
		article := articleBody{
			ID:          entry.Item.ID,
			Rank:        entry.Rank,
			Slot:        string(entry.Slot),
			Title:       entry.Item.Title,
			Link:        entry.Item.Link,
			Author:      entry.Item.Author,
			Summary:     entry.Item.Summary,
			ImageURL:    entry.Item.ImageURL,
			PublishedAt: entry.Item.PublishedAt.Unix(),
			Feed:        titles[entry.Item.FeedID],
		}
		if entry.Read() {
			at := entry.ReadAt.Unix()
			article.ReadAt = &at
		}
		if article.Feed.ID == "" {
			// The subscription went while the page was live. The article is still on it —
			// discarding a card because its source was unfollowed would leave a hole in a
			// layout that was decided at generation time.
			article.Feed = feedStub{ID: entry.Item.FeedID}
		}
		body.Items = append(body.Items, article)
	}
	writeJSON(w, http.StatusOK, body)
}

// regenerate composes a page now and rebases the clock from this moment, so a manual
// regeneration does not leave a stale timer about to fire.
func (s *Server) regenerate(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if _, err := s.gen.Regenerate(r.Context(), p.ID, s.store.Now()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.edition(w, r)
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request)   { s.setRead(w, r, true) }
func (s *Server) markUnread(w http.ResponseWriter, r *http.Request) { s.setRead(w, r, false) }

func (s *Server) setRead(w http.ResponseWriter, r *http.Request, read bool) {
	if err := s.store.SetRead(r.Context(), principalOf(r).ID, r.PathValue("id"), read); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
