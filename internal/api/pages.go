package api

import (
	"net/http"
	"time"

	"bystander/internal/store"
)

// pageBody is one page as the interface sees it.
//
// The filter lists are always present, even when the mode says nothing reads them. An absent
// list and an empty one would be the same JSON, and the interface has to be able to show what
// was last chosen while a mode is being switched about.
type pageBody struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	IsMain bool   `json:"is_main"`

	EditionInterval int64 `json:"edition_interval"` // seconds
	EditionSize     int   `json:"edition_size"`
	NextEditionAt   int64 `json:"next_edition_at"`
	MaxArticleAge   int64 `json:"max_article_age"` // seconds; 0 is no limit

	TagFilter  string   `json:"tag_filter"`
	FeedFilter string   `json:"feed_filter"`
	TagIDs     []string `json:"tag_ids"`
	FeedIDs    []string `json:"feed_ids"`
}

func pageOf(page *store.Page) pageBody {
	return pageBody{
		ID:              page.ID,
		Name:            page.Name,
		Slug:            page.Slug,
		IsMain:          page.IsMain,
		EditionInterval: int64(page.EditionInterval.Seconds()),
		EditionSize:     page.EditionSize,
		NextEditionAt:   page.NextEditionAt.Unix(),
		MaxArticleAge:   int64(page.ArticleWindow.Seconds()),
		TagFilter:       string(page.TagFilter),
		FeedFilter:      string(page.FeedFilter),
		TagIDs:          orEmpty(page.TagIDs),
		FeedIDs:         orEmpty(page.FeedIDs),
	}
}

// orEmpty turns a nil slice into an empty one, so the JSON says [] rather than null.
func orEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// listPages is the tab strip: every page this person has, main first.
func (s *Server) listPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.store.Pages(r.Context(), principalOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// The lists are loaded per page rather than in the query above, because the strip does not
	// need them and a page being edited is fetched on its own. This is a handful of rows for a
	// handful of pages, once, when the reader opens.
	out := make([]pageBody, 0, len(pages))
	for _, page := range pages {
		full, err := s.store.PageOf(r.Context(), page.PrincipalID, page.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out = append(out, pageOf(full))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.store.PageOf(r.Context(), principalOf(r).ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(page))
}

type createPageRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Server) createPage(w http.ResponseWriter, r *http.Request) {
	var body createPageRequest
	if !decode(w, r, &body) {
		return
	}

	page, err := s.store.CreatePage(r.Context(), principalOf(r).ID, body.Name, body.Slug)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("a page was created",
		"principal", principalOf(r).ID, "page", page.ID, "slug", page.Slug)
	writeJSON(w, http.StatusCreated, pageOf(page))
}

// patchPageRequest is everything about a page that can change, in one body.
//
// A nil field was not mentioned. The filter lists are the exception that proves it: a mode
// without its list is a page drawing from the wrong things, so the interface sends both, and the
// store applies them in one transaction.
type patchPageRequest struct {
	Name            *string   `json:"name"`
	Slug            *string   `json:"slug"`
	EditionInterval *int64    `json:"edition_interval"`
	EditionSize     *int      `json:"edition_size"`
	MaxArticleAge   *int64    `json:"max_article_age"`
	TagFilter       *string   `json:"tag_filter"`
	FeedFilter      *string   `json:"feed_filter"`
	TagIDs          *[]string `json:"tag_ids"`
	FeedIDs         *[]string `json:"feed_ids"`
}

func (s *Server) patchPage(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body patchPageRequest
	if !decode(w, r, &body) {
		return
	}

	// Resolved before the change so that a page belonging to somebody else is not found rather
	// than quietly edited — UpdatePage takes an id and knows nothing about who is asking.
	page, err := s.store.PageOf(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	patch := store.PagePatch{
		Name:        body.Name,
		Slug:        body.Slug,
		EditionSize: body.EditionSize,
	}
	if body.EditionInterval != nil {
		d := time.Duration(*body.EditionInterval) * time.Second
		patch.EditionInterval = &d
	}
	if body.MaxArticleAge != nil {
		d := time.Duration(*body.MaxArticleAge) * time.Second
		patch.ArticleWindow = &d
	}
	if body.TagFilter != nil {
		f := store.TagFilter(*body.TagFilter)
		patch.TagFilter = &f
	}
	if body.FeedFilter != nil {
		f := store.FeedFilter(*body.FeedFilter)
		patch.FeedFilter = &f
	}
	// A pointer to a slice, so that "clear this list" and "do not touch this list" are different
	// requests. Sending [] means empty; leaving the field out means leave it alone.
	if body.TagIDs != nil {
		patch.TagIDs = orEmpty(*body.TagIDs)
	}
	if body.FeedIDs != nil {
		patch.FeedIDs = orEmpty(*body.FeedIDs)
	}

	if err := s.store.UpdatePage(r.Context(), page.ID, patch); err != nil {
		s.fail(w, r, err)
		return
	}

	updated, err := s.store.PageOf(r.Context(), p.ID, page.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(updated))
}

func (s *Server) deletePage(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	page, err := s.store.PageOf(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeletePage(r.Context(), page.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("a page was removed", "principal", p.ID, "page", page.ID, "slug", page.Slug)
	writeJSON(w, http.StatusNoContent, nil)
}
