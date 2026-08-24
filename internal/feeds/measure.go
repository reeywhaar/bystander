package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"time"

	// Registered for their DecodeConfig, which is the whole point: each reads a header and
	// stops. Nothing here ever decodes a pixel.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

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
// this on its own, so some perfectly healthy servers will time out and be retried. They are
// retried twice more, hours apart, and if a picture is never measured the page uses the shape
// it drew and looks exactly as it does now.
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
func giveUp(ctx context.Context, st *store.Store, url string, err error) error {
	if mark := st.GiveUpOnImage(ctx, url); mark != nil {
		// The write failed, so the queue will offer this again. Returning the write error
		// rather than the drop keeps that visible instead of losing it behind a job that
		// looks deliberately abandoned.
		return mark
	}
	return fmt.Errorf("%w: %v", jobs.Drop, err)
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
			return giveUp(ctx, st, job.URL, err)
		}
		req.Header.Set("User-Agent", agent)
		req.Header.Set("Accept", "image/*")
		// Politeness first, and a real saving when it is honoured.
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", measureBudget-1))

		res, err := client.Do(req)
		if err != nil {
			// A refused connection, a DNS failure, a timeout. Some of these would be
			// different tomorrow and none of them is worth finding out.
			return giveUp(ctx, st, job.URL, err)
		}
		defer res.Body.Close()

		// Every answer other than the picture is the same answer: this one is not measured.
		// A 429 and a 404 differ in what they mean and not at all in what to do about them,
		// once nothing is being retried.
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
			return giveUp(ctx, st, job.URL, fmt.Errorf("%s said %s", job.URL, res.Status))
		}

		cfg, _, err := image.DecodeConfig(io.LimitReader(res.Body, measureBudget))
		if err != nil {
			// Not an image, or a format with no decoder registered — AVIF and SVG both land
			// here.
			return giveUp(ctx, st, job.URL, fmt.Errorf("could not read %s: %v", job.URL, err))
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return giveUp(ctx, st, job.URL,
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
		if err := runner.Enqueue(ctx, MeasureImage, MeasureImage+" "+url, string(payload)); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}
