package api

import (
	"cmp"
	"net/http"
	"slices"
)

// imageFailureBody is one reason pictures are unmeasured, and how many.
type imageFailureBody struct {
	// Reason is empty for pictures nothing has asked about yet, which is a queue that has
	// not caught up rather than a failure.
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type imagesBody struct {
	Pictures   int                `json:"pictures"`
	Measured   int                `json:"measured"`
	Unmeasured int                `json:"unmeasured"`
	Failures   []imageFailureBody `json:"failures"`
}

// images says how the pictures on this instance are getting on.
//
// A page draws a shape for a picture it has no measurements for, which looks like a design
// choice rather than a fault — so an instance can spend months quietly cropping half its
// comics and nothing says so. This is the screen that says so.
//
// The breakdown is the point rather than the total. A hundred failures that all say "refused"
// is one host with hotlink protection; a hundred that say "undecodable" is a format this build
// cannot read. Those are two very different afternoons, and a single count of failures cannot
// tell them apart.
func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	tally, err := s.store.Images(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	out := imagesBody{
		Pictures:   tally.Pictures,
		Measured:   tally.Measured,
		Unmeasured: tally.Unmeasured(),
		Failures:   make([]imageFailureBody, 0, len(tally.Failures)),
	}
	for reason, count := range tally.Failures {
		out.Failures = append(out.Failures, imageFailureBody{Reason: reason, Count: count})
	}
	// Biggest first, and by name within a tie: this is a list somebody reads to find out what
	// is wrong, and the largest group is the answer more often than not.
	slices.SortFunc(out.Failures, func(a, b imageFailureBody) int {
		if by := cmp.Compare(b.Count, a.Count); by != 0 {
			return by
		}
		return cmp.Compare(a.Reason, b.Reason)
	})
	writeJSON(w, http.StatusOK, out)
}

// unmeasuredImageBody is one picture behind a reason.
type unmeasuredImageBody struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
	// RetryAt is when the queue will ask again, or 0 for a picture already due.
	RetryAt  int64  `json:"retry_at"`
	Articles int    `json:"articles"`
	Title    string `json:"title"`
}

type unmeasuredImagesBody struct {
	Reason string `json:"reason"`
	// Limit is the ceiling this list was read under, sent so the client can tell a list that
	// was cut short from one that is simply shorter than the count beside its reason. Those
	// look identical and mean opposite things: the second is the queue having measured some
	// of them since the count was taken, which is the system working.
	Limit    int                   `json:"limit"`
	Pictures []unmeasuredImageBody `json:"pictures"`
}

// listLimit is how many pictures one reason will list.
//
// A ceiling on a screen rather than a page of results. The count beside the reason already
// says how many there are; this list is for recognising *which* — one host, or forty — and a
// hundred rows answers that as well as a thousand would.
const listLimit = 100

// unmeasuredImages lists the pictures behind one of the counts on the images screen.
//
// The counts say what is wrong. This says with what, which is what anybody who has read the
// counts wants next: forty pictures under "refused" is one host with hotlink protection or
// forty publishers each losing one, and only the addresses tell those apart.
//
// The reason arrives as a query parameter rather than a path segment because the empty one —
// pictures nothing has asked about yet — is a real group, and an empty path segment is not a
// path.
func (s *Server) unmeasuredImages(w http.ResponseWriter, r *http.Request) {
	reason := r.URL.Query().Get("reason")

	pictures, err := s.store.UnmeasuredByReason(r.Context(), reason, listLimit)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	out := unmeasuredImagesBody{
		Reason: reason, Limit: listLimit,
		Pictures: make([]unmeasuredImageBody, 0, len(pictures)),
	}
	for _, pic := range pictures {
		body := unmeasuredImageBody{
			URL: pic.URL, Reason: pic.Reason, Articles: pic.Articles, Title: pic.Title,
		}
		if !pic.RetryAt.IsZero() {
			body.RetryAt = pic.RetryAt.Unix()
		}
		out.Pictures = append(out.Pictures, body)
	}
	writeJSON(w, http.StatusOK, out)
}

type retryImagesRequest struct {
	// Reason narrows it to one kind of failure. Empty means every picture without a size.
	Reason string `json:"reason"`
	// URL narrows it to one picture, and takes precedence over Reason when both arrive —
	// asking about one address and a whole category in the same request is a request that
	// means two things, and the narrower of them is the one somebody pressed.
	URL string `json:"url"`
}

// retryImages offers unmeasured pictures back to the queue at once.
//
// The queue retries on its own — within the hour for a host that was busy, within the day for
// a settled answer — so this is not for a publisher who was down. It is for the times *this
// program* was wrong: a format it had no decoder for, a header it did not send. Waiting out a
// day per picture is a slow way to find out something a new build already knows.
//
// Nothing measured is touched. The size is what ended the asking, and no administrator wants a
// thousand requests to re-learn what is already recorded.
func (s *Server) retryImages(w http.ResponseWriter, r *http.Request) {
	var body retryImagesRequest
	if !decode(w, r, &body) {
		return
	}

	var queued int
	var err error
	if body.URL != "" {
		queued, err = s.store.RetryImage(r.Context(), body.URL)
	} else {
		queued, err = s.store.RetryImages(r.Context(), body.Reason)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("pictures were offered to the measuring queue again",
		"principal", principalOf(r).ID, "reason", cmp.Or(body.Reason, "any"),
		"url", cmp.Or(body.URL, "any"), "pictures", queued)
	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}
