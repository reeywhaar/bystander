package api

import (
	"errors"
	"net/http"

	"bystander/internal/store"
)

type instanceBody struct {
	PublicPages    bool `json:"public_pages"`
	PublicIndexing bool `json:"public_indexing"`
	Landing        bool `json:"landing"`
}

// getInstance reports the answers that belong to the instance rather than to anybody on it.
func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Instance(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, instanceBody{
		PublicPages:    settings.PublicPages,
		PublicIndexing: settings.PublicIndexing,
		Landing:        settings.Landing,
	})
}

// putInstance writes them.
//
// Turning publishing off takes every published page down at once, rather than only stopping new
// ones. It is the instance's answer to whether it serves anything to strangers, and an answer
// that only applied to pages published afterwards would not be one.
func (s *Server) putInstance(w http.ResponseWriter, r *http.Request) {
	var body instanceBody
	if !decode(w, r, &body) {
		return
	}
	if err := s.store.SetInstance(r.Context(), store.InstanceSettings{
		PublicPages:    body.PublicPages,
		PublicIndexing: body.PublicIndexing,
		Landing:        body.Landing,
	}); err != nil {
		s.fail(w, r, err)
		return
	}
	// The shell "/" serves is decided from this, and decided without a query — so the one
	// place it changes is the one place the cache is told. See showsLanding.
	s.landing.Store(&body.Landing)

	s.log.Info("the instance's public settings were changed",
		"principal", principalOf(r).ID, "public_pages", body.PublicPages,
		"public_indexing", body.PublicIndexing, "landing", body.Landing)
	s.getInstance(w, r)
}

type publishRequest struct {
	// Slug is the last part of the address: /p/<your public name>/<this>.
	Slug string `json:"slug"`
	// Indexable is the owner's answer about search engines. Ignored where the instance says
	// no, and the interface does not offer it there.
	Indexable bool `json:"indexable"`
}

// publishPage puts one of somebody's pages on the open web.
//
// Two things are checked before the write and they are checked separately, because they have
// different answers. An instance that does not publish anything is not something the person can
// fix; a missing public name is, and saying so is what lets the interface ask for one rather
// than refusing.
func (s *Server) publishPage(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body publishRequest
	if !decode(w, r, &body) {
		return
	}

	settings, err := s.store.Instance(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !settings.PublicPages {
		writeError(w, http.StatusForbidden, "this instance does not publish pages")
		return
	}

	// Resolved before the change so that a page belonging to somebody else is not found
	// rather than quietly published — PublishPage takes an id and knows nothing about who is
	// asking.
	page, err := s.store.PageOf(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	owner, err := s.store.PrincipalByID(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if owner.Slug == "" {
		writeError(w, http.StatusConflict, "choose a public name first")
		return
	}

	// The instance's answer is the ceiling, applied on the way in as well as on the way out:
	// a page that stored `indexable` while the instance said no would quietly become
	// indexable the day an administrator changed their mind.
	if err := s.store.PublishPage(r.Context(), page.ID,
		body.Slug, body.Indexable && settings.PublicIndexing); err != nil {
		s.fail(w, r, err)
		return
	}

	s.log.Info("a page was published",
		"principal", p.ID, "page", page.ID, "at", owner.Slug+"/"+body.Slug)
	s.writePage(w, r, p.ID, page.ID)
}

// unpublishPage takes a page down, and remembers where it was.
//
// The address is kept so that publishing it again offers the one the links already point at.
// Nobody reaches it in the meantime: what is served is decided by `published`, not by the
// presence of an address.
func (s *Server) unpublishPage(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	page, err := s.store.PageOf(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.UnpublishPage(r.Context(), page.ID); err != nil {
		s.fail(w, r, err)
		return
	}

	s.log.Info("a page was taken down", "principal", p.ID, "page", page.ID)
	s.writePage(w, r, p.ID, page.ID)
}

// writePage renders one of somebody's pages, read fresh.
//
// By id rather than from what the request was holding, for the same reason an account is: that
// copy was loaded before this request's writes, and answering with it looks exactly like a
// write that silently did nothing.
func (s *Server) writePage(w http.ResponseWriter, r *http.Request, principalID, pageID string) {
	page, err := s.store.PageOf(r.Context(), principalID, pageID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(page))
}

type publicPageBody struct {
	// Name is what the page is called. Whose it is appears in the address and nowhere else:
	// the public name is the only identity somebody chose to expose, and putting a username
	// beside it would expose one they did not.
	// ID is the edition's, and it is here for one reason: every card's appearance is drawn
	// from it. Seed the page with anything else and the same edition renders differently for
	// a stranger than for the person who published it — same articles, different faces,
	// different widths, different boxes — which is not a published page, it is a second one.
	ID          string `json:"id"`
	Name        string `json:"name"`
	GeneratedAt int64  `json:"generated_at"`
	// Indexable is whether a search engine may keep this. Both the owner and the instance
	// have to say yes, and this is the answer after both were asked.
	Indexable bool `json:"indexable"`
	// SignedIn is whether whoever asked has an account here.
	//
	// The page needs it to decide whether to offer a way to mark anything read: a control
	// that exists and refuses is worse than one that is not there, and a stranger has no
	// read state for it to act on.
	SignedIn bool          `json:"signed_in"`
	Items    []articleBody `json:"items"`
}

// publicPage serves somebody's published page to anybody at all.
//
// No session, and no read state: the articles arrive as articles rather than as somebody's
// reading. A visitor's own read marks are a separate question and a separate landing — what
// matters here is that the owner's are not on show. Whether they have read something is a fact
// about them, and publishing a page is not an offer to publish that too.
//
// Every way this can fail is not found — no such person, no such page, taken down, an account
// switched off, an instance that publishes nothing. A stranger has no business learning which,
// and an owner already knows.
func (s *Server) publicPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.store.PublishedPage(r.Context(), r.PathValue("person"), r.PathValue("page"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// Resolved rather than required, which is the one endpoint here that does. A session is
	// not needed to read this and is used when there is one: the read marks on the page are
	// then the visitor's own. Never the owner's — whether they have read something is a fact
	// about them, and publishing a page is not an offer to publish that too.
	viewer, err := s.sessions.Resolve(r.Context(), w, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var viewerID string
	if viewer != nil {
		viewerID = viewer.ID
	}

	body := publicPageBody{
		Name:      page.Name,
		Indexable: page.Indexable,
		SignedIn:  viewer != nil,
		Items:     []articleBody{},
	}

	ed, items, err := s.store.CurrentEdition(r.Context(), page.ID, viewerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// A published page that has not been composed yet is a page with nothing on
			// it, which the reader renders. It is not an error and it is not a 404: the
			// address is real, and saying otherwise would tell the owner their link is
			// broken when it is merely early.
			writeJSON(w, http.StatusOK, body)
			return
		}
		s.fail(w, r, err)
		return
	}

	// The owner's own names for their feeds, because those are what appear on their page.
	subs, err := s.store.ListSubscriptions(r.Context(), page.PrincipalID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Name and address only. The stub can carry the subscription behind a card, which is what
	// lets a reader act on their own feeds from the page — and these are somebody else's, being
	// handed to a stranger. There is nothing here for them to act on, so there is nothing to
	// send: see feedStub.SubscriptionID.
	titles := make(map[string]feedStub, len(subs))
	for _, sub := range subs {
		titles[sub.FeedID] = feedStub{ID: sub.FeedID, Title: sub.Title(), SiteURL: sub.Feed.SiteURL}
	}

	body.ID = ed.ID
	body.GeneratedAt = ed.GeneratedAt.Unix()
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
			ImageWidth:  entry.Item.ImageWidth,
			ImageHeight: entry.Item.ImageHeight,
			PublishedAt: entry.Item.PublishedAt.Unix(),
			Feed:        titles[entry.Item.FeedID],
		}
		// The visitor's own mark, or none. A stranger has no read state and every article
		// arrives unmarked, which is the difference between reading somebody's page and
		// reading their reading.
		if entry.Read() {
			at := entry.ReadAt.Unix()
			article.ReadAt = &at
		}
		body.Items = append(body.Items, article)
	}
	writeJSON(w, http.StatusOK, body)
}
