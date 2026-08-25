package api

import (
	"errors"
	"net/http"
	"slices"
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

	// Each side of each list, rather than a mode and one list. The tags are a funnel — draw
	// from these, then drop those — and the feeds override what comes out of it in both
	// directions. Anything the page has no opinion about is on neither side.
	IncludeTagIDs  []string `json:"include_tag_ids"`
	ExcludeTagIDs  []string `json:"exclude_tag_ids"`
	IncludeFeedIDs []string `json:"include_feed_ids"`
	ExcludeFeedIDs []string `json:"exclude_feed_ids"`

	// Where this page lives on the open web, and whether that address answers. The slug is
	// kept when a page is taken down, so publishing it again offers the address the links
	// already point at.
	PublishSlug string `json:"publish_slug"`
	Published   bool   `json:"published"`
	// Indexable is the owner's answer about search engines, after the instance's answer has
	// been applied to it: where the instance says no this is false whatever was stored.
	Indexable bool `json:"indexable"`
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
		IncludeTagIDs:   orEmpty(page.IncludeTagIDs),
		ExcludeTagIDs:   orEmpty(page.ExcludeTagIDs),
		IncludeFeedIDs:  orEmpty(page.IncludeFeedIDs),
		ExcludeFeedIDs:  orEmpty(page.ExcludeFeedIDs),
		PublishSlug:     page.PublishSlug,
		Published:       page.Published,
		Indexable:       page.Indexable,
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
	IncludeTagIDs   *[]string `json:"include_tag_ids"`
	ExcludeTagIDs   *[]string `json:"exclude_tag_ids"`
	IncludeFeedIDs  *[]string `json:"include_feed_ids"`
	ExcludeFeedIDs  *[]string `json:"exclude_feed_ids"`
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
	// A pointer to a slice, so that "clear this list" and "do not touch this list" are different
	// requests. Sending [] means empty; leaving the field out means leave it alone.
	if body.IncludeTagIDs != nil {
		patch.IncludeTagIDs = orEmpty(*body.IncludeTagIDs)
	}
	if body.ExcludeTagIDs != nil {
		patch.ExcludeTagIDs = orEmpty(*body.ExcludeTagIDs)
	}
	if body.IncludeFeedIDs != nil {
		patch.IncludeFeedIDs = orEmpty(*body.IncludeFeedIDs)
	}
	if body.ExcludeFeedIDs != nil {
		patch.ExcludeFeedIDs = orEmpty(*body.ExcludeFeedIDs)
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

	// A filter change is a change to what the page is made of, so it is composed again now.
	//
	// The alternative is a page that says it draws from one thing and displays another until
	// its next turn — which for a weekly page is six days of looking broken. Cadence and size
	// do not do this: they describe the next composition rather than this one, and recomposing
	// on a slider would spend the page somebody is reading.
	if drawsFromSomethingElse(page, updated) {
		s.recompose(r, updated)
	}

	writeJSON(w, http.StatusOK, pageOf(updated))
}

// drawsFromSomethingElse reports whether a save changed what a page may draw from.
//
// Only the filter and the window. A rename or a new address changes where a page is, not what
// is on it, and a cadence changed is a statement about the next composition rather than this
// one.
func drawsFromSomethingElse(before, after *store.Page) bool {
	return before.ArticleWindow != after.ArticleWindow ||
		!slices.Equal(before.IncludeTagIDs, after.IncludeTagIDs) ||
		!slices.Equal(before.ExcludeTagIDs, after.ExcludeTagIDs) ||
		!slices.Equal(before.IncludeFeedIDs, after.IncludeFeedIDs) ||
		!slices.Equal(before.ExcludeFeedIDs, after.ExcludeFeedIDs)
}

// recompose composes a page again after its filter changed, and never fails the save.
//
// The save has already happened and is what the caller asked for; composing is a consequence of
// it. A page that could not be composed is not a page that was not saved.
//
// When nothing can be composed, the old edition is thrown away rather than left up. It was
// chosen under the old filter and may hold nothing the new one would have picked, so keeping it
// would show somebody exactly what they have just said they do not want. Empty is the honest
// answer, and it is the same empty a page has before its first composition.
//
// Both of the ways composing declines mean the same thing here, which is worth being explicit
// about because they do not look alike. Regenerate says not-found when the page has no edition
// and nothing to make one from, and conflict when it *has* one and could not better it — and
// the second is the case this exists for: a filter narrowed to something that matches nothing,
// on a page that is currently showing yesterday's articles. Treating conflict as "leave it
// alone" would keep the stale page in precisely the situation the recompose was for.
func (s *Server) recompose(r *http.Request, page *store.Page) {
	_, err := s.gen.Regenerate(r.Context(), page.ID, s.store.Now())
	switch {
	case err == nil:
		return
	case !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrConflict):
		// Something actually went wrong. Leave the page alone: emptying it on a database
		// error would turn a transient fault into lost reading.
		s.log.Error("could not compose a page after its filter changed",
			"page", page.ID, "error", err)
		return
	}

	if err := s.store.DropEditions(r.Context(), page.ID); err != nil {
		s.log.Error("could not clear a page that has nothing to show",
			"page", page.ID, "error", err)
		return
	}
	s.log.Debug("a page's filter now matches nothing, so it was cleared", "page", page.ID)
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
