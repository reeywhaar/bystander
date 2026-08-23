package api

import (
	"bytes"
	"net/http"
	"strings"

	"bystander/internal/opml"
)

type createShareRequest struct {
	// IDs are subscription ids. Empty means everything, which is what the export means by
	// it too.
	IDs []string `json:"ids"`
}

type shareBody struct {
	URL       string `json:"url"`
	Count     int    `json:"count"`
	ExpiresAt int64  `json:"expires_at"`
}

// createShare turns a list of feeds into a link.
//
// Sharing was a file: export, save, send, and have the other person find it and paste it
// back. That works on a desk and falls apart between two phones, which is where people
// actually do this — standing next to each other, one of them holding a screen up.
//
// A snapshot, not a reference to what this person reads. Unfollowing something afterwards is
// not a reason for somebody else's link to change under them.
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	var body createShareRequest
	if !decode(w, r, &body) {
		return
	}

	doc, err := s.exportDocument(r, body.IDs)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	var buf bytes.Buffer
	if err := opml.Encode(&buf, doc); err != nil {
		s.fail(w, r, err)
		return
	}

	share, token, err := s.store.CreateShare(r.Context(), p.ID, buf.String(), len(doc.Feeds))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	s.log.Info("a list of feeds was shared", "principal", p.ID, "feeds", share.FeedCount)
	writeJSON(w, http.StatusCreated, shareBody{
		// Absolute, because the whole point is that this leaves the browser it was made
		// in: it goes into a message, or onto a screen as a square somebody else's camera
		// reads. A path would be useless in both.
		URL:       strings.TrimSuffix(s.cfg.PublicURL.String(), "/") + SharePath + "/" + token,
		Count:     share.FeedCount,
		ExpiresAt: share.ExpiresAt.Unix(),
	})
}

type sharedListBody struct {
	// From is who made the link. A list of feeds is a recommendation, and a recommendation
	// with no name on it is a list of URLs.
	From      string        `json:"from"`
	ExpiresAt int64         `json:"expires_at"`
	Feeds     []previewFeed `json:"feeds"`
}

// share is what a link holds, in the shape the picker already reads.
//
// Nothing here adds a feed. Opening a link shows what is in it; taking any of it goes
// through the import endpoint, the same one a pasted file uses — so a link cannot subscribe
// anybody to anything merely by being opened.
//
// Deliberately the same `previewFeed` an imported file produces, through the same planning
// code: after "where did these come from", the question is identical — which of them do I
// want, and filed under what. A second shape would have meant a second selection screen, and
// two screens that do the same job drift.
func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)

	shared, err := s.store.ShareByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// Who shared it, looked up now rather than stored on the row: a name can change, and a
	// link that keeps saying somebody's old one is a link that ages badly.
	from, err := s.store.PrincipalByID(r.Context(), shared.PrincipalID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	doc, err := opml.DecodeAny(shared.OPML)
	if err != nil {
		// Written by this program, so this cannot be a bad file — it is a bug or a
		// corrupted row, and either way it is ours rather than the reader's.
		s.fail(w, r, err)
		return
	}

	following, err := s.following(r, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	feeds, err := s.plan(r, doc.Feeds, following)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, sharedListBody{
		From:      from.Username,
		ExpiresAt: shared.ExpiresAt.Unix(),
		Feeds:     feeds,
	})
}
