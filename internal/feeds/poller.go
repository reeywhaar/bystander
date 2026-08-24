package feeds

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"bystander/internal/store"
)

const (
	// batch is how many feeds one cycle takes. A ceiling on the work a single tick can
	// start, not a limit on how many feeds may exist: whatever is left is still due and
	// is taken next cycle.
	batch = 100

	// workers is how many fetches run at once. Feeds are on other people's servers and
	// most of the time here is waiting, so this is about not appearing as a flood rather
	// than about CPU.
	workers = 6

	// maxBackoff caps the retry delay for a feed that keeps failing. A dead feed is
	// retried occasionally rather than every cycle forever, and a feed that comes back is
	// noticed within six hours.
	maxBackoff = 6 * time.Hour
)

// Poller keeps feeds fetched.
type Poller struct {
	store    *store.Store
	fetcher  *Fetcher
	interval time.Duration
	log      *slog.Logger
}

func NewPoller(st *store.Store, fetcher *Fetcher, interval time.Duration, log *slog.Logger) *Poller {
	return &Poller{store: st, fetcher: fetcher, interval: interval, log: log}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// Once at startup: a feed added while the service was down, or one whose retry came
	// due, should not wait for the first tick.
	p.cycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.cycle(ctx)
		}
	}
}

// cycle fetches whatever is due.
//
// Due-ness is per feed and lives in the feeds table, so the ticker here only decides how
// often to look. That is what lets a newly added feed be fetched within the minute while
// a feed that failed five times waits hours.
func (p *Poller) cycle(ctx context.Context) {
	due, err := p.store.DueFeeds(ctx, batch)
	if err != nil {
		if ctx.Err() == nil {
			p.log.Error("could not find out which feeds are due", "error", err)
		}
		return
	}
	if len(due) == 0 {
		return
	}

	queue := make(chan *store.Feed)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for feed := range queue {
				p.fetchOne(ctx, feed)
			}
		}()
	}
	for _, feed := range due {
		if ctx.Err() != nil {
			break
		}
		queue <- feed
	}
	close(queue)
	wg.Wait()
}

func (p *Poller) fetchOne(ctx context.Context, feed *store.Feed) {
	now := p.store.Now()

	result, err := p.fetcher.Fetch(ctx, feed, now)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		status := 0
		if result != nil {
			status = result.Status
		}
		next := now.Add(p.backoff(feed.FailureCount))
		if err := p.store.RecordFailure(ctx, feed.ID, status, err.Error(), next); err != nil {
			p.log.Error("could not record a failed fetch", "feed", feed.ID, "error", err)
		}
		// Info, not Error: a feed being unreachable is ordinary and outside our control.
		// It becomes worth attention through failure_count, which the manage page shows.
		p.log.Info("could not fetch a feed",
			"feed", feed.ID, "url", feed.CanonicalURL, "failures", feed.FailureCount+1, "error", err)
		return
	}

	next := now.Add(p.interval)

	if result.NotModified {
		if err := p.store.RecordSuccess(ctx, feed.ID, "", "", result.ETag, result.LastModified, result.Status, next); err != nil {
			p.log.Error("could not record an unchanged fetch", "feed", feed.ID, "error", err)
		}
		return
	}

	added, err := p.store.SaveItems(ctx, result.Parsed.Items)
	if err != nil {
		p.log.Error("could not save articles", "feed", feed.ID, "error", err)
		// The fetch worked; the write did not. Recording it as a failure would back the
		// feed off for a problem at our end, so leave the schedule alone and try again
		// next cycle.
		return
	}

	if err := p.store.RecordSuccess(ctx, feed.ID,
		result.Parsed.Title, result.Parsed.SiteURL, result.ETag, result.LastModified, result.Status, next); err != nil {
		p.log.Error("could not record a fetch", "feed", feed.ID, "error", err)
		return
	}
	if added > 0 {
		p.log.Debug("fetched a feed", "feed", feed.ID, "new", added, "seen", len(result.Parsed.Items))
	}
	if feed.FailureCount > 0 {
		p.log.Info("a feed is working again", "feed", feed.ID, "url", feed.CanonicalURL, "after", feed.FailureCount)
	}
}

// backoff is how long to wait before retrying a feed that has failed.
//
// Exponential from the ordinary interval, capped. Doubling from the interval rather than
// from a fixed base means a service polling hourly does not retry a broken feed every
// thirty seconds, and one polling every five minutes does not wait a day.
func (p *Poller) backoff(failures int) time.Duration {
	delay := p.interval
	for range failures {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}
