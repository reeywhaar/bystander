package api

import (
	"errors"
	"net/http"

	"bystander/internal/feeds"
	"bystander/internal/store"
)

// subscriptionBody is a feed as its follower sees it: what they chose, plus what the
// fetcher learned.
//
// last_error is here because "this feed has gone quiet" without "and here is why" sends
// somebody to logs they do not have.
type subscriptionBody struct {
	ID            string   `json:"id"`
	URL           string   `json:"url"`
	SiteURL       string   `json:"site_url"`
	Title         string   `json:"title"`
	TitleOverride string   `json:"title_override"`
	Priority      int      `json:"priority"`
	TagIDs        []string `json:"tag_ids"`
	CreatedAt     int64    `json:"created_at"`

	LastSuccessAt *int64 `json:"last_success_at"`
	LastError     string `json:"last_error"`
	FailureCount  int    `json:"failure_count"`
}

func subscriptionOf(sub *store.Subscription) subscriptionBody {
	body := subscriptionBody{
		ID:            sub.ID,
		URL:           sub.Feed.CanonicalURL,
		SiteURL:       sub.Feed.SiteURL,
		Title:         sub.Title(),
		TitleOverride: sub.TitleOverride,
		Priority:      sub.Priority,
		TagIDs:        sub.TagIDs,
		CreatedAt:     sub.CreatedAt.Unix(),
		LastError:     sub.Feed.LastError,
		FailureCount:  sub.Feed.FailureCount,
	}
	// An empty slice rather than null, so the client never has to guard a map over it.
	if body.TagIDs == nil {
		body.TagIDs = []string{}
	}
	if !sub.Feed.LastSuccessAt.IsZero() {
		at := sub.Feed.LastSuccessAt.Unix()
		body.LastSuccessAt = &at
	}
	return body
}

func (s *Server) listFeeds(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.ListSubscriptions(r.Context(), principalOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]subscriptionBody, 0, len(subs))
	for _, sub := range subs {
		out = append(out, subscriptionOf(sub))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getFeed(w http.ResponseWriter, r *http.Request) {
	sub, err := s.store.SubscriptionByID(r.Context(), principalOf(r).ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionOf(sub))
}

type addFeedRequest struct {
	URL      string   `json:"url"`
	Priority *int     `json:"priority"`
	TagIDs   []string `json:"tag_ids"`
}

// addFeed resolves what somebody typed, then subscribes them to it.
//
// The URL is fetched before it is accepted, and a web page is followed to the feed it
// names. Somebody pasting example.com and being told "that is not a feed" when the feed is
// one hop away is a bad first minute, and it is the first minute everybody has.
func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body addFeedRequest
	if !decode(w, r, &body) {
		return
	}
	if !s.discovery.allow(p.ID) {
		writeError(w, http.StatusTooManyRequests, "too many feeds at once; wait a minute")
		return
	}

	feedURL, parsed, err := s.fetcher.Discover(r.Context(), body.URL, s.store.Now())
	if err != nil {
		if errors.Is(err, feeds.ErrNotAFeed) || errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// A publisher being down is not our failure, and a 500 would say it was.
		writeError(w, http.StatusBadRequest, "could not read that feed: "+err.Error())
		return
	}

	feed, err := s.store.UpsertFeed(r.Context(), feedURL, parsed.Title, parsed.SiteURL)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	priority := store.DefaultPriority
	if body.Priority != nil {
		priority = *body.Priority
	}
	sub, err := s.store.Subscribe(r.Context(), p.ID, feed.ID, priority, body.TagIDs)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// The articles this discovery already parsed, saved under the real feed id — so a
	// page can be composed immediately rather than after the poller's next cycle.
	for _, item := range parsed.Items {
		item.FeedID = feed.ID
	}
	if _, err := s.store.SaveItems(r.Context(), parsed.Items); err != nil {
		// The subscription is real either way; the poller will fetch again shortly.
		s.log.Warn("could not save the articles from a newly added feed", "feed", feed.ID, "error", err)
	}

	s.log.Info("a feed was added", "principal", p.ID, "feed", feed.ID, "url", feed.CanonicalURL)
	writeJSON(w, http.StatusCreated, subscriptionOf(sub))
}

type patchFeedRequest struct {
	Priority      *int      `json:"priority"`
	TitleOverride *string   `json:"title_override"`
	TagIDs        *[]string `json:"tag_ids"`
}

func (s *Server) patchFeed(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body patchFeedRequest
	if !decode(w, r, &body) {
		return
	}
	if err := s.store.UpdateSubscription(r.Context(), p.ID, r.PathValue("id"),
		body.Priority, body.TitleOverride, body.TagIDs); err != nil {
		s.fail(w, r, err)
		return
	}

	sub, err := s.store.SubscriptionByID(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionOf(sub))
}

func (s *Server) deleteFeed(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSubscription(r.Context(), principalOf(r).ID, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
