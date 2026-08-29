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
	ID       string `json:"id"`
	Rank     int    `json:"rank"`
	Slot     string `json:"slot"`
	ReadAt   *int64 `json:"read_at"`
	Title    string `json:"title"`
	Link     string `json:"link"`
	Author   string `json:"author"`
	Summary  string `json:"summary"`
	ImageURL string `json:"image_url"`
	// ImageWidth and ImageHeight are the picture's real size, or zero when nothing has
	// measured it — which is the ordinary case for anything just published. The page falls
	// back to a shape drawn from the article's id, so zero is not a missing value the client
	// has to work around.
	ImageWidth  int      `json:"image_width"`
	ImageHeight int      `json:"image_height"`
	PublishedAt int64    `json:"published_at"`
	Feed        feedStub `json:"feed"`
}

type feedStub struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	SiteURL string `json:"site_url"`

	// SubscriptionID is how the reader acts on the feed behind a card — showing more or less
	// of it, or being done with it — without going to the feed list to find it.
	//
	// The subscription's id and not the feed's, because that is what every endpoint that
	// changes a feed is keyed on: a feed is one row for the whole instance, and what somebody
	// can change is their own following of it.
	//
	// Empty where there is nothing to act on: an article whose subscription went while the
	// page was live, and every article on somebody else's published page — see publish.go,
	// which builds its stubs from the owner's subscriptions and must not hand their ids to
	// a stranger.
	SubscriptionID string `json:"subscription_id"`
	// Priority is how often this feed is drawn, so the reader can say what showing more or
	// less of it would mean rather than moving a number nobody can see.
	Priority int `json:"priority"`
}

func (s *Server) edition(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	page, err := s.store.PageOf(r.Context(), p.ID, r.URL.Query().Get("page"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	ed, items, err := s.store.CurrentEdition(r.Context(), page.ID, p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Not a 404. "Your page has not been generated yet" is a state the reader
			// renders, not an error it reports — and a 404 on the one endpoint the front
			// page calls would send the interface into its failure path on a new
			// account's very first visit.
			writeJSON(w, http.StatusOK, editionBody{
				NextEditionAt: page.NextEditionAt.Unix(),
				Size:          page.EditionSize,
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
		titles[sub.FeedID] = feedStub{
			ID:             sub.FeedID,
			Title:          sub.Title(),
			SiteURL:        sub.Feed.SiteURL,
			SubscriptionID: sub.ID,
			Priority:       sub.Priority,
		}
	}

	body := editionBody{
		ID:            ed.ID,
		GeneratedAt:   ed.GeneratedAt.Unix(),
		NextEditionAt: page.NextEditionAt.Unix(),
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
			ImageWidth:  entry.Item.ImageWidth,
			ImageHeight: entry.Item.ImageHeight,
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
	page, err := s.store.PageOf(r.Context(), principalOf(r).ID, r.URL.Query().Get("page"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.gen.Regenerate(r.Context(), page.ID, s.store.Now()); err != nil {
		s.fail(w, r, err)
		return
	}
	// Answers with the page it just composed, which is the one the caller asked for — s.edition
	// reads the same query parameter.
	s.edition(w, r)
}

type readArticleBody struct {
	ItemID      string   `json:"item_id"`
	Title       string   `json:"title"`
	Link        string   `json:"link"`
	PublishedAt int64    `json:"published_at"`
	ReadAt      int64    `json:"read_at"`
	Feed        feedStub `json:"feed"`
}

// readArticles is what somebody has read lately — a month of it, and nothing older.
//
// Deliberately not a count of anything. It lists only what has been dealt with, which is
// the one kind of list that asks nothing of the person reading it.
func (s *Server) readArticles(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	articles, err := s.store.ReadArticles(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// Feed titles live in the other database and cannot be joined to, so they are resolved
	// here — and only for feeds still followed. Something read and then unsubscribed from
	// keeps its own title and loses its source, which is the honest rendering of it.
	subs, err := s.store.ListSubscriptions(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	titles := make(map[string]feedStub, len(subs))
	for _, sub := range subs {
		titles[sub.FeedID] = feedStub{
			ID:             sub.FeedID,
			Title:          sub.Title(),
			SiteURL:        sub.Feed.SiteURL,
			SubscriptionID: sub.ID,
			Priority:       sub.Priority,
		}
	}

	out := make([]readArticleBody, 0, len(articles))
	for _, a := range articles {
		out = append(out, readArticleBody{
			ItemID:      a.ItemID,
			Title:       a.Title,
			Link:        a.Link,
			PublishedAt: a.PublishedAt.Unix(),
			ReadAt:      a.ReadAt.Unix(),
			Feed:        titles[a.FeedID],
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request)   { s.setRead(w, r, true) }
func (s *Server) markUnread(w http.ResponseWriter, r *http.Request) { s.setRead(w, r, false) }

func (s *Server) setRead(w http.ResponseWriter, r *http.Request, read bool) {
	// Any article, and no check that it is on a page of yours.
	//
	// There used to be one, and it guarded something that no longer exists: a read mark was
	// once a column on the edition, so writing it meant writing to a particular page. It is
	// now a fact about a person and an article, stored once against the person doing the
	// reading — so the worst an id from nowhere can do is mark something read for the person
	// who sent it, which is not a thing worth refusing.
	//
	// That is also what lets this one endpoint serve a page somebody else published. For
	// somebody signed in, such a page has the same controls as their own — they simply
	// cannot compose a new one, because it is not theirs to compose.
	if err := s.store.SetRead(r.Context(), principalOf(r).ID, r.PathValue("id"), read); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
