package api

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"bystander/internal/app"
	"bystander/internal/opml"
	"bystander/internal/store"
)

// MaxImport bounds one import. Far past any real subscription list, and near enough that a
// pasted file cannot ask this to create ten thousand rows.
const MaxImport = 500

type exportRequest struct {
	// IDs are subscription ids. Empty means everything, which is what "select all" sends
	// rather than enumerating.
	IDs []string `json:"ids"`
}

// exportFeeds writes a subscription list.
//
// A POST rather than a GET with the ids in the query, because "everything I follow" can be
// a hundred ids and a URL is a poor place to put them. The response is the file itself, so
// the interface can show it to be copied or hand it over as a download without a second
// round trip.
func (s *Server) exportFeeds(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body exportRequest
	if !decode(w, r, &body) {
		return
	}

	subs, err := s.store.ListSubscriptions(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	wanted := make(map[string]bool, len(body.IDs))
	for _, id := range body.IDs {
		wanted[id] = true
	}

	// Tag paths are resolved once per tag rather than once per use: a taxonomy is small,
	// and a feed with four tags would otherwise walk the same ancestry four times.
	paths := make(map[string][]string)
	tags, err := s.store.ListTags(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, tag := range tags {
		path, err := s.store.TagPath(r.Context(), p.ID, tag.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		paths[tag.ID] = path
	}

	doc := opml.Document{
		Title:     p.Username + "'s feeds",
		OwnerName: p.Username,
		CreatedAt: s.store.Now(),
	}
	for _, sub := range subs {
		if len(wanted) > 0 && !wanted[sub.ID] {
			continue
		}
		feed := opml.Feed{
			Title:    sub.Title(),
			FeedURL:  sub.Feed.CanonicalURL,
			SiteURL:  sub.Feed.SiteURL,
			Priority: sub.Priority,
		}
		for _, tagID := range sub.TagIDs {
			if path := paths[tagID]; len(path) > 0 {
				feed.Categories = append(feed.Categories, path)
			}
		}
		doc.Feeds = append(doc.Feeds, feed)
	}

	var buf bytes.Buffer
	if err := opml.Encode(&buf, doc); err != nil {
		s.fail(w, r, err)
		return
	}

	// JSON carrying the document, rather than the document itself with a
	// Content-Disposition. This API is JSON in and JSON out, and one endpoint that is not
	// would mean the dispatcher had to know which — see private/docs/api_design.md. The
	// interface shows the text to be copied and builds a download from the same string,
	// so nothing is lost by handing it over this way.
	writeJSON(w, http.StatusOK, map[string]any{
		"opml":     buf.String(),
		"filename": app.Name + "-" + p.Username + ".opml",
		"count":    len(doc.Feeds),
	})
}

type importRequest struct {
	OPML string `json:"opml"`
}

type previewTag struct {
	Path []string `json:"path"`
	Name string   `json:"name"`
	// Existing is whether this person already has this tag. The interface shows the ones
	// they have as their own and the rest as new, so nobody is surprised by a taxonomy
	// appearing.
	Existing bool `json:"existing"`
}

type previewFeed struct {
	Title             string       `json:"title"`
	FeedURL           string       `json:"feed_url"`
	SiteURL           string       `json:"site_url"`
	Priority          int          `json:"priority"`
	AlreadySubscribed bool         `json:"already_subscribed"`
	Tags              []previewTag `json:"tags"`
}

// previewImport says what a file would do, without doing any of it.
//
// Two phases because an import is somebody else's decisions arriving in bulk: which feeds,
// filed under which names, at which priorities. Applying that unseen is how a person ends
// up with a taxonomy they did not choose and cannot easily unpick. So this answers the two
// questions worth asking first — what is already here, and which of these tags are mine —
// and the interface lets them untick whatever they do not want.
func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body importRequest
	if !decode(w, r, &body) {
		return
	}

	doc, err := opml.Decode(strings.NewReader(body.OPML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(doc.Feeds) == 0 {
		writeError(w, http.StatusBadRequest, "there are no feeds in that list")
		return
	}
	if len(doc.Feeds) > MaxImport {
		writeError(w, http.StatusBadRequest, "that list holds more feeds than this can import at once")
		return
	}

	subs, err := s.store.ListSubscriptions(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Matched on the canonical URL rather than the one written in the file, so a list that
	// says http:// for a feed somebody already follows over https does not offer it twice.
	following := make(map[string]bool, len(subs))
	for _, sub := range subs {
		following[sub.Feed.CanonicalURL] = true
	}

	out := make([]previewFeed, 0, len(doc.Feeds))
	for _, feed := range doc.Feeds {
		canonical, err := store.CanonicalURL(feed.FeedURL)
		if err != nil {
			// A line that is not a URL is not worth refusing the whole file over.
			continue
		}

		entry := previewFeed{
			Title:             strings.TrimSpace(feed.Title),
			FeedURL:           canonical,
			SiteURL:           feed.SiteURL,
			Priority:          feed.Priority,
			AlreadySubscribed: following[canonical],
			Tags:              make([]previewTag, 0, len(feed.Categories)),
		}
		if entry.Title == "" {
			entry.Title = canonical
		}
		if entry.Priority < 0 {
			entry.Priority = store.DefaultPriority
		}

		for _, path := range feed.Categories {
			existing, err := s.store.TagByPath(r.Context(), p.ID, path)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			entry.Tags = append(entry.Tags, previewTag{
				Path:     path,
				Name:     strings.Join(path, " / "),
				Existing: existing != nil,
			})
		}
		out = append(out, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{"feeds": out})
}

type importFeed struct {
	FeedURL  string     `json:"feed_url"`
	Title    string     `json:"title"`
	SiteURL  string     `json:"site_url"`
	Priority *int       `json:"priority"`
	TagPaths [][]string `json:"tag_paths"`
}

type importResult struct {
	Added   int             `json:"added"`
	Skipped int             `json:"skipped"`
	Failed  []importFailure `json:"failed"`
	Tags    []string        `json:"tags_created"`
}

type importFailure struct {
	FeedURL string `json:"feed_url"`
	Error   string `json:"error"`
}

// importFeeds subscribes to what the interface says it wants.
//
// Exactly that, and nothing more: the client sends the feeds it kept and the tag paths it
// kept, so unticking a feed is simply not sending it. This endpoint has no opinion about
// what was in the file — that was the preview's job — which is what keeps "what I chose"
// and "what happened" the same thing.
//
// No feed is fetched here. A hundred subscriptions would be a hundred outbound requests
// and several minutes of somebody watching a spinner, and the poller is about to fetch
// them all anyway; the title from the file stands in until it does.
func (s *Server) importFeeds(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body struct {
		Feeds []importFeed `json:"feeds"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.Feeds) == 0 {
		writeError(w, http.StatusBadRequest, "nothing was selected")
		return
	}
	if len(body.Feeds) > MaxImport {
		writeError(w, http.StatusBadRequest, "that is more feeds than this can import at once")
		return
	}

	result := importResult{Failed: []importFailure{}, Tags: []string{}}
	created := map[string]bool{}

	for _, wanted := range body.Feeds {
		feed, err := s.store.UpsertFeed(r.Context(), wanted.FeedURL, wanted.Title, wanted.SiteURL)
		if err != nil {
			result.Failed = append(result.Failed, importFailure{FeedURL: wanted.FeedURL, Error: err.Error()})
			continue
		}

		tagIDs := make([]string, 0, len(wanted.TagPaths))
		for _, path := range wanted.TagPaths {
			before, err := s.store.TagByPath(r.Context(), p.ID, path)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			tag, err := s.store.EnsureTagPath(r.Context(), p.ID, path)
			if err != nil {
				result.Failed = append(result.Failed, importFailure{FeedURL: wanted.FeedURL, Error: err.Error()})
				continue
			}
			if tag == nil {
				continue
			}
			if before == nil && !created[tag.ID] {
				created[tag.ID] = true
				result.Tags = append(result.Tags, strings.Join(path, " / "))
			}
			tagIDs = append(tagIDs, tag.ID)
		}

		priority := store.DefaultPriority
		if wanted.Priority != nil {
			priority = *wanted.Priority
		}

		if _, err := s.store.Subscribe(r.Context(), p.ID, feed.ID, priority, tagIDs); err != nil {
			// Already following it is the ordinary outcome of importing a list twice, and
			// is not a failure worth reporting as one.
			if errors.Is(err, store.ErrConflict) {
				result.Skipped++
				continue
			}
			result.Failed = append(result.Failed, importFailure{FeedURL: wanted.FeedURL, Error: err.Error()})
			continue
		}
		result.Added++
	}

	s.log.Info("imported a subscription list",
		"principal", p.ID, "added", result.Added, "skipped", result.Skipped, "failed", len(result.Failed))
	writeJSON(w, http.StatusOK, result)
}
