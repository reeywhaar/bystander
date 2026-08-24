package api

import (
	"errors"
	"net/http"
	"time"

	"bystander/internal/feeds"
	"bystander/internal/opml"
	"bystander/internal/store"
)

// subscriptionBody is a feed as its follower sees it: what they chose, plus what the
// fetcher learned.
//
// last_error is here because "this feed has gone quiet" without "and here is why" sends
// somebody to logs they do not have.
type subscriptionBody struct {
	ID string `json:"id"`
	// FeedID is the feed itself, which is a different thing from following it. A page's feed
	// filter names feeds, because two people following one feed are following one feed.
	FeedID  string `json:"feed_id"`
	URL     string `json:"url"`
	SiteURL string `json:"site_url"`
	// Title is what to call this feed here: the override if there is one, the publisher's
	// otherwise.
	Title string `json:"title"`
	// FeedTitle is what the publisher calls it, always — so a rename can show what it is
	// overriding, and offer to put it back.
	FeedTitle     string   `json:"feed_title"`
	TitleOverride string   `json:"title_override"`
	Priority      int      `json:"priority"`
	TagIDs        []string `json:"tag_ids"`
	CreatedAt     int64    `json:"created_at"`

	// ArticleWindow is how old an article from this feed may be and still reach a page,
	// in seconds. Zero is no limit.
	ArticleWindow int64 `json:"article_window"`

	LastSuccessAt *int64 `json:"last_success_at"`
	// LastStatus is what the server answered with, or zero when the request never reached one.
	// That distinction is the first thing somebody asking "why is this not answering" needs,
	// and it cannot be read off the message.
	LastStatus int    `json:"last_status"`
	LastError  string `json:"last_error"`
	// LastErrorBody is what the server said when it refused, so a reader can be shown the
	// thing itself rather than a summary of it. Empty when nothing answered.
	LastErrorBody string `json:"last_error_body"`
	FailureCount  int    `json:"failure_count"`
}

func subscriptionOf(sub *store.Subscription) subscriptionBody {
	body := subscriptionBody{
		ID:            sub.ID,
		FeedID:        sub.FeedID,
		URL:           sub.Feed.CanonicalURL,
		SiteURL:       sub.Feed.SiteURL,
		Title:         sub.Title(),
		FeedTitle:     sub.Feed.Title,
		TitleOverride: sub.TitleOverride,
		Priority:      sub.Priority,
		TagIDs:        sub.TagIDs,
		ArticleWindow: int64(sub.ArticleWindow.Seconds()),
		CreatedAt:     sub.CreatedAt.Unix(),
		LastStatus:    sub.Feed.LastStatus,
		LastError:     sub.Feed.LastError,
		LastErrorBody: sub.Feed.LastErrorBody,
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

	feedURL, parsed, err := s.fetcher.Resolve(r.Context(), body.URL, s.store.Now())
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
	sub, err := s.store.Subscribe(r.Context(), p.ID, feed.ID, priority, store.DefaultArticleWindow, body.TagIDs)
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

type discoverRequest struct {
	URL string `json:"url"`
}

// discoverFeeds says what a URL turns out to be, without subscribing to anything.
//
// A site usually names more than one feed — posts, comments, a podcast, one per category —
// and handing somebody whichever came first in the markup is how they end up subscribed to
// a comments feed they did not want. So the interface asks. `POST /api/feeds` still guesses
// for a caller that did not ask to be consulted.
func (s *Server) discoverFeeds(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body discoverRequest
	if !decode(w, r, &body) {
		return
	}
	// Shares the ceiling on adding a feed: this is the endpoint that actually makes the
	// outbound request, so it is the one that needs limiting.
	if !s.discovery.allow(p.ID) {
		writeError(w, http.StatusTooManyRequests, "too many feeds at once; wait a minute")
		return
	}

	found, err := s.fetcher.Discover(r.Context(), body.URL, s.store.Now())
	if err != nil {
		if errors.Is(err, feeds.ErrNotAFeed) {
			writeError(w, http.StatusBadRequest, "that page does not offer a feed")
			return
		}
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// A publisher being unreachable is not our failure, and a 500 would say it was.
		writeError(w, http.StatusBadRequest, "could not read that address: "+err.Error())
		return
	}

	// The same shape a pasted list produces, because after "where did these come from" the
	// question is the same both times: which of them do I want, and filed under what. One
	// shape means one selection screen rather than two that drift.
	following, err := s.following(r, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	feeds := make([]opml.Feed, 0, len(found.Candidates))
	for _, candidate := range found.Candidates {
		feeds = append(feeds, opml.Feed{
			Title:   candidate.Title,
			FeedURL: candidate.URL,
			SiteURL: found.PageURL,
			// A feed found in a site's markup carries no tags, no priority and no reach;
			// the defaults apply and the interface offers every tag this person has.
			Priority: -1,
			Reach:    -1,
		})
	}

	out, err := s.plan(r, feeds, following)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out})
}

type patchFeedRequest struct {
	Priority      *int      `json:"priority"`
	TitleOverride *string   `json:"title_override"`
	TagIDs        *[]string `json:"tag_ids"`
	ArticleWindow *int64    `json:"article_window"`
}

func (s *Server) patchFeed(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body patchFeedRequest
	if !decode(w, r, &body) {
		return
	}
	patch := store.SubscriptionPatch{
		Priority:      body.Priority,
		TitleOverride: body.TitleOverride,
		TagIDs:        body.TagIDs,
	}
	if body.ArticleWindow != nil {
		window := time.Duration(*body.ArticleWindow) * time.Second
		patch.ArticleWindow = &window
	}
	if err := s.store.UpdateSubscription(r.Context(), p.ID, r.PathValue("id"), patch); err != nil {
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

// The spans somebody can mark read, by what an article is older than.
//
// A closed set, like every other duration this program offers: four choices fit in a dialog and
// an arbitrary date is a date picker nobody asked for. "Everything" is the empty string, which
// is the whole feed rather than a bound of zero.
var markSpans = map[string]time.Duration{
	"day":   24 * time.Hour,
	"week":  7 * 24 * time.Hour,
	"month": 30 * 24 * time.Hour,
}

type markReadRequest struct {
	// OlderThan is "day", "week", "month", or empty for everything.
	OlderThan string `json:"older_than"`
}

// markFeedRead marks a feed's articles read, as far back as was asked for.
//
// It covers articles no page has shown yet, which is the point rather than a side effect:
// a page never offers what this person has already read, so this is how somebody who has been
// reading a publisher elsewhere — or who followed it again after a while — starts from now
// instead of from its backlog.
func (s *Server) markFeedRead(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body markReadRequest
	if !decode(w, r, &body) {
		return
	}

	// Through the subscription, so this can only ever mark a feed the caller follows — the
	// store's own call takes a feed id and knows nothing about who is asking.
	sub, err := s.store.SubscriptionByID(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	var before time.Time
	if body.OlderThan != "" {
		span, ok := markSpans[body.OlderThan]
		if !ok {
			writeError(w, http.StatusBadRequest, "that is not one of the spans that can be marked read")
			return
		}
		before = s.store.Now().Add(-span)
	}

	marked, err := s.store.MarkFeedRead(r.Context(), p.ID, sub.FeedID, before)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("marked a feed read",
		"principal", p.ID, "feed", sub.FeedID, "older_than", body.OlderThan, "articles", marked)

	writeJSON(w, http.StatusOK, map[string]int64{"marked": marked})
}

// unmarkFeedRead forgets that the caller read anything from a feed.
//
// A DELETE on the same place a POST marks read, because that is what it is: removing the read
// state rather than setting a different one. It takes no span — "unread the last week" is a
// question nobody has asked and one whose answer would be hard to predict, since the record
// says when something was read rather than how old it was.
func (s *Server) unmarkFeedRead(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	sub, err := s.store.SubscriptionByID(r.Context(), p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	forgotten, err := s.store.UnmarkFeedRead(r.Context(), p.ID, sub.FeedID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("marked a feed unread",
		"principal", p.ID, "feed", sub.FeedID, "articles", forgotten)

	writeJSON(w, http.StatusOK, map[string]int64{"marked": forgotten})
}
