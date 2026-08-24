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

// outcome is what became of one fetch.
type outcome int

const (
	// failed means the fetch did not work: unreachable, refused, or unparseable. Ordinary,
	// and the feed backs off rather than being given up on.
	failed outcome = iota
	// unchanged means the publisher answered 304. Most fetches are this once a feed has been
	// followed for a day, and it is the cheapest possible answer.
	unchanged
	// fetched means articles came back, new or not.
	fetched
)

// cycleTally counts what one pass over the due feeds came to.
//
// Guarded by its own mutex because the fetches run in parallel — six of them — and a count
// that is only nearly right is worse than no count, since it is the number somebody would use
// to decide nothing is wrong.
type cycleTally struct {
	mu                         sync.Mutex
	fetched, unchanged, failed int
	articles                   int
}

func (t *cycleTally) add(o outcome, articles int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch o {
	case fetched:
		t.fetched++
		t.articles += articles
	case unchanged:
		t.unchanged++
	case failed:
		t.failed++
	}
}

func (t *cycleTally) any() bool { return t.fetched+t.unchanged+t.failed > 0 }

// Poller keeps feeds fetched.
type Poller struct {
	store   *store.Store
	fetcher *Fetcher
	log     *slog.Logger
}

func NewPoller(st *store.Store, fetcher *Fetcher, log *slog.Logger) *Poller {
	return &Poller{store: st, fetcher: fetcher, log: log}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// One line to say the poller is there, beside "databases open" and "job queue open".
	//
	// Without it an instance whose feeds are all up to date prints nothing about fetching at
	// all, and "quiet because there is nothing due" is indistinguishable from "not running" —
	// which is exactly the question somebody restarting a server is asking.
	p.log.Info("feed poller open",
		"looks", time.Minute, "fastest", MinFetchInterval, "slowest", MaxFetchInterval)

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

	started := time.Now()
	var tally cycleTally

	queue := make(chan *store.Feed)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for feed := range queue {
				tally.add(p.fetchOne(ctx, feed))
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

	if ctx.Err() == nil {
		p.report(&tally, len(due), time.Since(started))
	}
}

// report says what a cycle came to.
//
// At Info only when something happened that a person would want to know about — articles
// arrived, or a feed refused. A cycle that found forty feeds unchanged is the ordinary state
// of a working instance and says nothing; at Info it would be a line a minute that nobody
// reads, which is how a log stops being read at all.
//
// Liveness is answered by the line at startup instead, so quiet here means quiet rather than
// stopped.
func (p *Poller) report(t *cycleTally, due int, took time.Duration) {
	if !t.any() {
		return
	}

	at := []any{
		"fetched", t.fetched,
		"unchanged", t.unchanged,
		"failed", t.failed,
		"articles", t.articles,
		"took", took.Round(time.Millisecond),
	}
	// A full batch means there was more due than one cycle takes, and the rest waits a
	// minute. Worth saying, because it is the difference between keeping up and not.
	if due >= batch {
		at = append(at, "capped_at", batch)
	}

	if t.articles > 0 || t.failed > 0 {
		p.log.Info("polled the feeds", at...)
		return
	}
	p.log.Debug("polled the feeds", at...)
}

// fetchOne fetches one feed and says what became of it, and how many articles were new.
func (p *Poller) fetchOne(ctx context.Context, feed *store.Feed) (outcome, int) {
	now := p.store.Now()
	started := time.Now()

	result, err := p.fetcher.Fetch(ctx, feed, now)
	took := time.Since(started).Round(time.Millisecond)
	if err != nil {
		if ctx.Err() != nil {
			return failed, 0
		}
		// The status and what the server said, when there was a server. A request that never
		// reached one leaves both empty, and a zero status is what says so.
		status, body := 0, ""
		if result != nil {
			status, body = result.Status, result.ErrorBody
		}
		next := now.Add(p.backoff(feed.FailureCount))
		if err := p.store.RecordFailure(ctx, feed.ID, status, err.Error(), body, next); err != nil {
			p.log.Error("could not record a failed fetch", "feed", feed.ID, "error", err)
		}
		// Info, not Error: a feed being unreachable is ordinary and outside our control.
		// It becomes worth attention through failure_count, which the manage page shows.
		p.log.Info("could not fetch a feed", "feed", feed.ID, "url", feed.CanonicalURL,
			"failures", feed.FailureCount+1, "took", took, "error", err)
		return failed, 0
	}

	if result.NotModified {
		// Nothing came back to work a cadence out from, so the one from the last fetch that
		// did bring articles stands. A publisher saying "unchanged" is not telling us they
		// have changed how often they publish.
		interval := feed.FetchInterval
		if interval <= 0 {
			interval = UnknownFetchInterval
		}
		if err := p.store.RecordSuccess(ctx, feed.ID, "", "", result.ETag, result.LastModified,
			result.Status, interval, now.Add(interval)); err != nil {
			p.log.Error("could not record an unchanged fetch", "feed", feed.ID, "error", err)
		}
		p.log.Debug("a feed is unchanged", "feed", feed.ID, "took", took, "again_in", interval)
		return unchanged, 0
	}

	added, err := p.store.SaveItems(ctx, result.Parsed.Items)
	if err != nil {
		p.log.Error("could not save articles", "feed", feed.ID, "error", err)
		// The fetch worked; the write did not. Recording it as a failure would back the
		// feed off for a problem at our end, so leave the schedule alone and try again
		// next cycle.
		//
		// Counted as failed all the same: the cycle did not come away with the articles, and
		// a summary saying it did would be the summary lying.
		return failed, 0
	}

	// How often to come back, from what this feed has just published — see Cadence. Worked
	// out on every fetch that brings articles, so a publisher who speeds up or goes quiet is
	// followed rather than assumed.
	published := make([]time.Time, 0, len(result.Parsed.Items))
	for _, item := range result.Parsed.Items {
		published = append(published, item.PublishedAt)
	}
	interval := Cadence(published)

	if err := p.store.RecordSuccess(ctx, feed.ID,
		result.Parsed.Title, result.Parsed.SiteURL, result.ETag, result.LastModified,
		result.Status, interval, now.Add(interval)); err != nil {
		p.log.Error("could not record a fetch", "feed", feed.ID, "error", err)
		return failed, 0
	}
	// Every fetch leaves a line at Debug now, not only the ones that brought something. How
	// long a publisher took is the thing a count cannot tell you, and "the poller is slow" and
	// "one publisher is slow" look identical in a total.
	p.log.Debug("fetched a feed", "feed", feed.ID, "new", added,
		"seen", len(result.Parsed.Items), "took", took, "again_in", interval)

	if feed.FailureCount > 0 {
		p.log.Info("a feed is working again", "feed", feed.ID, "url", feed.CanonicalURL, "after", feed.FailureCount)
	}
	return fetched, added
}

// backoff is how long to wait before retrying a feed that has failed.
//
// Exponential from the fastest this program polls anything, capped. Deliberately not from the
// feed's own cadence: a weekly comic that stops answering should be checked again in an hour,
// not in a fortnight. How often a publisher writes and how long they have been unreachable are
// different questions.
func (p *Poller) backoff(failures int) time.Duration {
	delay := time.Duration(MinFetchInterval)
	for range failures {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}
