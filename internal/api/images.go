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

type retryImagesRequest struct {
	// Reason narrows it to one kind of failure. Empty means every picture without a size.
	Reason string `json:"reason"`
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

	queued, err := s.store.RetryImages(r.Context(), body.Reason)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("pictures were offered to the measuring queue again",
		"principal", principalOf(r).ID, "reason", cmp.Or(body.Reason, "any"), "pictures", queued)
	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}
