package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	// Registered for their DecodeConfig, which is the whole point: each reads a header and
	// stops. Nothing here ever decodes a pixel.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	// WebP, which the standard library has no decoder for and half the web now serves. Two of
	// the four pictures that genuinely could not be measured on a real instance were WebP —
	// one from a stock CDN, one from an art magazine — and both measure fine with this.
	//
	// Registered for its side effect like the three above it: nothing here decodes an image,
	// it only reads the header, and image.DecodeConfig picks the decoder by sniffing.
	_ "golang.org/x/image/webp"

	"bystander/internal/jobs"
	"bystander/internal/store"
)

// MeasureImage is the kind of job this file handles.
const MeasureImage = "image.measure"

// measureBudget is how much of a picture is read before giving up on its header.
//
// A picture's dimensions live in its header, not spread through it: PNG puts them in the first
// twenty-four bytes, GIF in the first ten, JPEG in a marker usually inside the first few
// kilobytes. Sixty-four is generous for all three and still refuses to pull a four-megabyte
// photograph over somebody's connection to learn two numbers.
const measureBudget = 64 << 10

// measureTimeout caps one measurement.
//
// Five seconds, covering the connection, the handshake and the header. It is deliberately
// short: nothing is waiting on this, a host that needs longer can be asked again tomorrow, and
// holding a connection open to a slow server is holding it open on their side too.
//
// The cost is real and worth knowing — a cold TLS handshake to a distant host can take most of
// this on its own, so some perfectly healthy servers will time out. They are asked again within
// the hour, which is what makes a short timeout affordable; this comment used to claim a retry
// that did not exist, and every picture that timed out here was never measured again. See
// MeasureRetrySoon.
const measureTimeout = 5 * time.Second

type measurePayload struct {
	URL string `json:"url"`
}

// give up on a picture, once and for all, whatever went wrong.
//
// Every failure ends here. There is no retry and no backoff: a picture nothing could measure
// costs nothing, because the page draws a shape for it and looks exactly as it does now, so
// no outcome is worth a second request to somebody else's server. A host that happens to be
// down during its one moment is never measured, and that is a page that looks like the page
// already looks.
// How long a picture is left alone after each kind of failure.
//
// Two answers, because the failures are two kinds. A refused connection, a timeout, a 429 or a
// 5xx are all a host saying "not now" — it was having a moment, or it thinks we are asking too
// often, and an hour later it will very likely answer. A 404, a body that is not an image, a
// format with no decoder: those are settled answers, and asking again within the hour would
// just be the same answer at the host's expense.
//
// Neither is permanent, and that is the point — this used to be a flag that meant never again.
// A 404 today is a picture that moved; an undecodable format today is a format nothing had a
// decoder for until somebody added one, which is exactly what happened to WebP here.
const (
	MeasureRetrySoon  = time.Hour
	MeasureRetryLater = 24 * time.Hour
)

// Why a picture could not be measured, as a category rather than a message.
//
// A message is for reading once. A category is something a later version of this program can
// act on: add a decoder for a format there was none for, and the migration that adds it can
// re-offer every picture that failed as Undecodable and nothing else. Without that the choice
// is between re-probing the whole database and re-probing none of it.
//
// Kept deliberately coarse. These are the distinctions that change what anybody would *do*;
// finer ones would be a taxonomy nobody reads.
const (
	// Gone is a 404 or a 410: the host is fine and the picture is not there.
	Gone = "gone"
	// Refused is a 401 or a 403 — hotlink protection, most often.
	Refused = "refused"
	// Busy is a 429 or a 5xx: the host is having a moment and said so.
	Busy = "busy"
	// Unreachable is a request that never got an answer — DNS, a refused connection, or the
	// five-second timeout, which a cold handshake to a distant host can spend on its own.
	Unreachable = "unreachable"
	// Undecodable is an answer that arrived and was not a picture this build can read. AVIF
	// and SVG land here; WebP did until a decoder was registered for it.
	Undecodable = "undecodable"
	// Empty is a picture that decoded and claimed no size.
	Empty = "empty"
)

// postpone records that a picture could not be measured, why, and when to try it again.
func postpone(ctx context.Context, st *store.Store, url, reason string, after time.Duration, err error) error {
	if mark := st.PostponeImage(ctx, url, reason, after); mark != nil {
		// The write failed, so the queue will offer this again. Returning the write error
		// rather than the drop keeps that visible instead of losing it behind a job that
		// looks deliberately abandoned.
		return mark
	}
	return fmt.Errorf("%w: %v", jobs.Drop, err)
}

// retryAfter is how long a host asked to be left alone, when it said.
//
// Sent with 429 and 503, in seconds or as a date, and worth honouring: a host that troubles to
// say when it will be ready is a host worth not annoying. Bounded at both ends — a zero or a
// date in the past would mean asking straight back, and some hosts answer with a day and a
// half, which is longer than the article will be interesting for.
func retryAfter(res *http.Response, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(res.Header.Get("Retry-After"))
	if raw == "" {
		return fallback
	}

	var wait time.Duration
	if seconds, err := strconv.Atoi(raw); err == nil {
		wait = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(raw); err == nil {
		wait = time.Until(at)
	} else {
		return fallback
	}

	if wait < time.Minute {
		return time.Minute
	}
	if wait > MeasureRetryLater {
		return MeasureRetryLater
	}
	return wait
}

// Measure asks how big a picture is, without downloading it.
//
// `image.DecodeConfig` reads exactly as far as the header and returns — so this is one request
// that is closed after a few kilobytes, not a download. A `Range` header asks the host to send
// only that much in the first place; hosts that ignore it are handled by closing the body,
// which drops the rest of the transfer on the floor.
//
// It is a job rather than part of the fetch for one reason: a feed with thirty new articles is
// thirty pictures, and asking a publisher for thirty things the moment they publish is how a
// reader's address ends up blocked. As a job it is a few every sweep, spread over an hour that
// nobody is waiting through — the page is already correct without any of this.
func Measure(st *store.Store, agent string) jobs.Handler {
	client := &http.Client{Timeout: measureTimeout}

	return func(ctx context.Context, payload string) error {
		var job measurePayload
		if err := json.Unmarshal([]byte(payload), &job); err != nil {
			// Written by an older version of this program, and unreadable now. There is no
			// URL to mark, and nothing will change that.
			return fmt.Errorf("%w: unreadable payload: %v", jobs.Drop, err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, job.URL, nil)
		if err != nil {
			// A URL that will not even become a request. Nothing about it will be different
			// in an hour, so it waits out the long one.
			return postpone(ctx, st, job.URL, Undecodable, MeasureRetryLater, err)
		}
		req.Header.Set("User-Agent", agent)
		req.Header.Set("Accept", "image/*")
		// Politeness first, and a real saving when it is honoured.
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", measureBudget-1))

		res, err := client.Do(req)
		if err != nil {
			// A refused connection, a DNS failure, a timeout — most often the timeout,
			// which is five seconds and which a cold handshake to a distant host can spend
			// on its own. All of them are "not now" rather than "not ever".
			return postpone(ctx, st, job.URL, Unreachable, MeasureRetrySoon, err)
		}
		defer res.Body.Close()

		// Every answer other than the picture means this one is not measured now. How long
		// it stays that way is the difference between a host in trouble and a settled
		// answer: a 429 or a 5xx is a bad minute and is asked again within the hour — with
		// whatever the host itself said, when it said — and everything else waits a day.
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
			reason, wait := Refused, MeasureRetryLater
			switch {
			case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
				reason, wait = Busy, retryAfter(res, MeasureRetrySoon)
			case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
				reason = Gone
			}
			return postpone(ctx, st, job.URL, reason, wait,
				fmt.Errorf("%s said %s", job.URL, res.Status))
		}

		cfg, _, err := image.DecodeConfig(io.LimitReader(res.Body, measureBudget))
		if err != nil {
			// Not an image, or a format with no decoder registered — AVIF and SVG both land
			// here. A settled answer for today, and not for ever: WebP was in this list
			// until a decoder was registered for it.
			return postpone(ctx, st, job.URL, Undecodable, MeasureRetryLater,
				fmt.Errorf("could not read %s: %v", job.URL, err))
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return postpone(ctx, st, job.URL, Empty, MeasureRetryLater,
				fmt.Errorf("%s is %dx%d", job.URL, cfg.Width, cfg.Height))
		}

		if err := st.SetImageSize(ctx, job.URL, cfg.Width, cfg.Height); err != nil {
			// The measurement worked and the write did not — worth another go.
			return err
		}
		return nil
	}
}

// QueueImageMeasurements adds jobs for pictures nothing has measured yet.
//
// Bounded per pass rather than draining the table, so a first run against a fresh database
// does not enqueue several thousand requests in one go. What is left over is picked up next
// sweep; there is no hurry, because the page is right either way.
func QueueImageMeasurements(ctx context.Context, st *store.Store, runner *jobs.Runner, limit int) (int, error) {
	urls, err := st.UnmeasuredImages(ctx, limit)
	if err != nil {
		return 0, err
	}

	queued := 0
	for _, url := range urls {
		payload, err := json.Marshal(measurePayload{URL: url})
		if err != nil {
			continue
		}
		// The URL is the identity: one picture, one measurement, however many articles use
		// it. Enqueueing an existing job leaves the existing one alone, backoff included.
		// The URL is both the identity and the label: for a picture they are the same string,
		// which is the only reason the queue got away without a label for as long as it did.
		if err := runner.Enqueue(ctx, MeasureImage, MeasureImage+" "+url, url, string(payload)); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}
