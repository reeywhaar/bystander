package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"bystander/internal/jobs"
	"bystander/internal/store"
)

// FetchFeed is the kind of job this file handles.
const FetchFeed = "feed.fetch"

// maxBackoff caps the retry delay for a feed that keeps failing. A dead feed is retried
// occasionally rather than every cycle forever, and a feed that comes back is noticed within
// six hours.
const maxBackoff = 6 * time.Hour

// fetchPayload is what a queued fetch carries: which feed, and nothing else.
//
// Not the URL, though the URL is what gets requested. A feed can be renamed or have its address
// corrected between a job being queued and being run, and the job should fetch whatever the
// feed is now rather than whatever it was a minute ago.
type fetchPayload struct {
	FeedID string `json:"feed_id"`
}

// Fetch fetches one feed, as a job.
//
// This was a poller: a ticker, a batch, a pool of six, and a tally, all of it built here. It is
// the same shape the picture measurer already had, which was the argument for moving it — not
// that a queue fetches better, but that two pieces of code doing background work should not
// each have their own answer to how to log it, how to retry it, and how to survive a restart.
// What comes out of the move is that both are visible in the same place, in the same words,
// which is what makes a queue something an operator can be shown rather than something they
// have to be told about.
//
// **Retries stay with the feed, not with the job.** The queue is told not to retry this kind at
// all — [store.Feed.NextFetchAt] is already a schedule, and a second one would be two clocks
// disagreeing about the same feed. A failure is recorded, the feed backs off, and it comes round
// again as ordinary due work. So a fetch job is always its one attempt, and always finishes.
func Fetch(st *store.Store, fetcher *Fetcher, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, payload string) error {
		var job fetchPayload
		if err := json.Unmarshal([]byte(payload), &job); err != nil {
			// Written by an older version of this program and unreadable now. There is no
			// feed to blame and nothing will change that.
			return fmt.Errorf("%w: unreadable payload: %v", jobs.Drop, err)
		}

		feed, err := st.FeedByID(ctx, job.FeedID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// Unfollowed by the last person following it between the job being queued
				// and being run. Ordinary, and nothing to retry.
				return fmt.Errorf("%w: the feed is gone", jobs.Drop)
			}
			return err
		}
		return fetchOne(ctx, st, fetcher, log, feed)
	}
}

// fetchOne fetches one feed and records what became of it.
func fetchOne(ctx context.Context, st *store.Store, fetcher *Fetcher, log *slog.Logger, feed *store.Feed) error {
	now := st.Now()

	result, err := fetcher.Fetch(ctx, feed, now)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The status and what the server said, when there was a server. A request that never
		// reached one leaves both empty, and a zero status is what says so.
		status, body := 0, ""
		if result != nil {
			status, body = result.Status, result.ErrorBody
		}
		next := now.Add(backoff(feed.FailureCount))
		if err := st.RecordFailure(ctx, feed.ID, status, err.Error(), body, next); err != nil {
			log.Error("could not record a failed fetch", "feed", feed.ID, "error", err)
		}
		// Returned rather than logged here. A feed being unreachable is ordinary and outside
		// our control, and the queue says so in the same words it uses for every other kind of
		// work — with the feed's name, which is the part anybody reading needs. How bad it has
		// got is failure_count, which the manage page shows and a log line cannot.
		return fmt.Errorf("%w: again in %s", err, backoff(feed.FailureCount).Round(time.Minute))
	}

	if result.NotModified {
		// Nothing came back to work a cadence out from, so the one from the last fetch that
		// did bring articles stands. A publisher saying "unchanged" is not telling us they
		// have changed how often they publish.
		interval := feed.FetchInterval
		if interval <= 0 {
			interval = UnknownFetchInterval
		}
		if err := st.RecordSuccess(ctx, feed.ID, "", "", result.ETag, result.LastModified,
			result.Status, interval, now.Add(interval)); err != nil {
			return fmt.Errorf("record an unchanged fetch: %w", err)
		}
		log.Debug("a feed is unchanged", "feed", feed.ID, "again_in", interval)
		return nil
	}

	added, err := st.SaveItems(ctx, result.Parsed.Items)
	if err != nil {
		// The fetch worked; the write did not. Deliberately not recorded as a failure: that
		// would back the feed off for a problem at our end. The schedule is left alone, which
		// leaves the feed due, so it is queued again on the next look.
		return fmt.Errorf("save articles: %w", err)
	}

	// How often to come back, from what this feed has just published — see Cadence. Worked out
	// on every fetch that brings articles, so a publisher who speeds up or goes quiet is
	// followed rather than assumed.
	published := make([]time.Time, 0, len(result.Parsed.Items))
	for _, item := range result.Parsed.Items {
		published = append(published, item.PublishedAt)
	}
	interval := Cadence(published)

	if err := st.RecordSuccess(ctx, feed.ID,
		result.Parsed.Title, result.Parsed.SiteURL, result.ETag, result.LastModified,
		result.Status, interval, now.Add(interval)); err != nil {
		return fmt.Errorf("record a fetch: %w", err)
	}
	log.Debug("fetched a feed", "feed", feed.ID,
		"new", added, "seen", len(result.Parsed.Items), "again_in", interval)

	// A state change nothing else can report: the queue sees one successful job and has no
	// idea it is the first in a fortnight.
	if feed.FailureCount > 0 {
		log.Info("a feed is working again",
			"feed", feed.ID, "url", feed.CanonicalURL, "after", feed.FailureCount)
	}
	return nil
}

// QueueDueFeeds lines up a fetch for every feed that is due, and says how many.
//
// The schedule lives in the feeds table rather than in the queue, and this is the join between
// them. That is deliberate: next_fetch_at is per feed, is set from how often that publisher
// actually writes, and has to survive the queue being drained — a fetch is not a piece of work
// somebody asked for, it is a standing arrangement.
//
// So the queue never holds more than the feeds due right now, and holds nothing at all on an
// instance that has caught up.
func QueueDueFeeds(ctx context.Context, st *store.Store, runner *jobs.Runner, limit int) (int, error) {
	due, err := st.DueFeeds(ctx, limit)
	if err != nil {
		return 0, err
	}

	queued := 0
	for _, feed := range due {
		payload, err := json.Marshal(fetchPayload{FeedID: feed.ID})
		if err != nil {
			continue
		}
		// The feed is the identity: one fetch per feed, however many people follow it.
		// Enqueueing one that is already queued leaves the existing row alone.
		if err := runner.Enqueue(ctx, FetchFeed, FetchFeed+" "+feed.ID, feedLabel(feed), string(payload)); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

// feedLabel is how a feed is named in a log line or a queue screen.
//
// The title, which is what somebody following it calls it, falling back to the address for a
// feed that has never been fetched and so has never told us its name.
func feedLabel(feed *store.Feed) string {
	if feed.Title != "" {
		return feed.Title
	}
	return feed.CanonicalURL
}

// backoff is how long to wait before retrying a feed that has failed.
//
// Exponential from the fastest this program fetches anything, capped. Deliberately not from the
// feed's own cadence: a weekly comic that stops answering should be checked again in an hour,
// not in a fortnight. How often a publisher writes and how long they have been unreachable are
// different questions.
func backoff(failures int) time.Duration {
	delay := time.Duration(MinFetchInterval)
	for range failures {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}
