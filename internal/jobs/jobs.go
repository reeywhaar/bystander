// Package jobs runs the background work that is nobody's request.
//
// Everything this program does when no one is looking shares one shape: it is about one thing,
// it can fail, it should be tried again and then left alone, and it must survive a restart.
// Measuring a picture. Fetching a feed. Sending a mail nobody is waiting on. That is a queue,
// and writing it once is cheaper than each of those growing its own attempts column, its own
// backoff, and its own idea of what to log.
//
// What it is not is one queue. Fetching feeds and measuring pictures are both outbound requests
// and want opposite things: a picture is one request every three seconds because the constraint
// is not getting a reader's address blocked, and a feed is six at a time every minute because
// the constraint is not falling behind the people publishing. Sharing a single pace meant
// choosing which of them to get wrong — a hundred queued pictures ahead of a due feed delayed
// that feed by five minutes, and running feeds at the pictures' width would have been a flood.
//
// So the queue is one table and many loops: a [Policy] per kind, each with its own ticker, its
// own width, and its own idea of how many times to try. The table is shared because the
// durability, the retry accounting, and the log lines are the same everywhere; the pace is not
// shared because it is the one thing that genuinely differs.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"bystander/internal/store"
)

// Drop is returned by a handler for work that will not succeed by being tried again.
//
// A 404, a 403, a body that is not what it claimed to be. Retrying those is asking somebody
// else's server to keep saying no on a schedule, which is both rude and pointless. Anything
// else a handler returns is treated as bad luck and tried again later.
var Drop = errors.New("drop this job")

// Handler does one job. The payload is whatever was enqueued with it.
type Handler func(ctx context.Context, payload string) error

// Policy is how one kind of work is paced.
//
// Every field has a default that suits a small amount of polite outbound work, so a kind that
// does not care states nothing. The defaults are the picture measurer's, because that is the
// shape most background work turns out to have.
type Policy struct {
	// Every is how often this kind's queue is looked at, and with a Batch and Concurrency of
	// one it is also the whole rate limit: at most one outbound request this often.
	Every time.Duration

	// Batch is how many jobs one pass takes.
	//
	// One is a rate limit, not a throughput ceiling: work arrives in bursts — a feed with
	// thirty new articles is thirty pictures, all on one host — and a pass that took eight of
	// them would make eight requests to the same server inside a second. Bursts are what a
	// host rate-limits.
	//
	// A kind whose work is spread across hosts wants the opposite: take the lot, run it as
	// wide as Concurrency allows, and be finished before the next tick.
	Batch int

	// Concurrency is how many of this kind run at once. Most of the time in any of this is
	// spent waiting on somebody else's server, so this is about how much of a crowd we look
	// like rather than about CPU.
	Concurrency int

	// MaxAttempts is how many times a job is tried before it is dropped.
	//
	// One means the queue never retries — right for work that already has a schedule of its
	// own, where a second retry mechanism would fight the first. A feed that fails backs off
	// in the feeds table and comes round again as ordinary due work; retrying its job as well
	// would be two clocks disagreeing about the same feed.
	MaxAttempts int

	// Backoff is how long to wait after each failed attempt, the last entry repeating.
	Backoff []time.Duration

	// RefillEvery is how often [Work.Refill] is asked for more, and only when this kind's
	// queue has run dry.
	RefillEvery time.Duration
}

// Defaults for a kind that does not say: one polite request every three seconds, three tries
// ten minutes then an hour then a day apart, looking for more work once a minute.
//
// Three seconds drains a feed's worth of pictures over a couple of minutes and would take an
// hour to make the number of requests a browser makes opening one news site. Nobody is waiting
// on any of it — the page is complete before a single job runs.
//
// Ten minutes, an hour, a day: the first failure is usually something passing — a timeout, a
// rate limit that has already reset — and by the third it is usually the answer.
//
// A minute for the refill, and only when the queue is empty, is slow enough that an idle
// instance is idle and quick enough that nothing waits meaningfully.
var Default = Policy{
	Every:       3 * time.Second,
	Batch:       1,
	Concurrency: 1,
	MaxAttempts: 3,
	Backoff:     []time.Duration{10 * time.Minute, time.Hour, 24 * time.Hour},
	RefillEvery: time.Minute,
}

// withDefaults fills in what a registration left unsaid.
func (p Policy) withDefaults() Policy {
	if p.Every <= 0 {
		p.Every = Default.Every
	}
	if p.Batch <= 0 {
		p.Batch = Default.Batch
	}
	if p.Concurrency <= 0 {
		p.Concurrency = Default.Concurrency
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = Default.MaxAttempts
	}
	if len(p.Backoff) == 0 {
		p.Backoff = Default.Backoff
	}
	if p.RefillEvery <= 0 {
		p.RefillEvery = Default.RefillEvery
	}
	return p
}

// wait is how long to hold a job that has failed this many times.
func (p Policy) wait(attempts int) time.Duration {
	return p.Backoff[min(attempts, len(p.Backoff)-1)]
}

// Work is everything the runner needs to know about one kind of job.
type Work struct {
	// Handle does one job.
	Handle Handler

	// Refill is asked for more work whenever this kind's queue has run dry, and returns how
	// much it queued.
	//
	// One mechanism rather than a hook wherever work might appear. Hooks were the first
	// design and there were three of them — after a poll, after the hourly sweep, at startup
	// — and they still missed the commonest case, because adding a feed through the interface
	// saves its articles directly and never goes near a fetch, so a new feed's pictures
	// waited an hour. A question the runner asks cannot be forgotten by a code path that did
	// not know it was supposed to answer, and it covers the abrupt restart for free: whatever
	// is in the table is picked up regardless of who put it there.
	Refill func(ctx context.Context) (int, error)

	// Policy is how this kind is paced. The zero value is [Default].
	Policy Policy
}

// Runner holds the registered work and runs what is due.
type Runner struct {
	store *store.Store
	log   *slog.Logger

	work map[string]Work
}

func New(st *store.Store, log *slog.Logger) *Runner {
	return &Runner{store: st, log: log, work: map[string]Work{}}
}

// Handle registers how a kind of work is done and paced.
//
// Panics on a second registration for the same kind, because that is two pieces of code each
// believing they own it and the wrong one would win silently.
func (r *Runner) Handle(kind string, w Work) {
	if _, taken := r.work[kind]; taken {
		panic("jobs: two handlers registered for " + kind)
	}
	w.Policy = w.Policy.withDefaults()
	r.work[kind] = w
}

// Outcome is what became of one job.
type Outcome int

const (
	// Waiting means nothing ran: the queue was empty, or the sweep was cut short.
	Waiting Outcome = iota
	// Done means the handler succeeded and the job is gone.
	Done
	// GaveUp means the job will not be tried again — the handler said so, or it ran out of
	// attempts. Not always an error: for pictures it is the ordinary answer from a host that
	// would rather not be asked.
	GaveUp
	// Later means it failed and is queued for another go.
	Later
)

// tally counts what a run of one kind's queue did, so the summary can say more than "some".
//
// Guarded, because a kind may run several jobs at once and a count that is only nearly right
// is worse than no count — it is the number somebody would use to decide nothing is wrong.
type tally struct {
	mu                  sync.Mutex
	done, gaveUp, later int
}

func (t *tally) add(o Outcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch o {
	case Done:
		t.done++
	case GaveUp:
		t.gaveUp++
	case Later:
		t.later++
	}
}

// merge adds another run's counts into this one.
func (t *tally) merge(o *tally) {
	o.mu.Lock()
	done, gaveUp, later := o.done, o.gaveUp, o.later
	o.mu.Unlock()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.done += done
	t.gaveUp += gaveUp
	t.later += later
}

// outcome flattens a run to the single word that best describes it, for a caller that only
// wants to know whether anything happened.
func (t *tally) outcome() Outcome {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t.later > 0:
		return Later
	case t.done > 0:
		return Done
	case t.gaveUp > 0:
		return GaveUp
	}
	return Waiting
}

func (t *tally) any() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done+t.gaveUp+t.later > 0
}

// Sweep works through what is due for one kind, and says whether it did anything.
func (r *Runner) Sweep(ctx context.Context, kind string) (Outcome, error) {
	run, err := r.sweep(ctx, kind)
	return run.outcome(), err
}

// sweep works through what is due for one kind and counts all of it.
//
// The count is the return value rather than a single outcome, because a sweep is no longer one
// job. Pictures take one at a time, so for them the two are the same thing and the difference
// never showed; feeds take a hundred, six at a time, and a summary built from one outcome per
// sweep said "done=1" after fetching nineteen of them. A tally that undercounts by nineteen to
// one is worse than no tally, because it is the number somebody would use to decide the queue
// is barely moving.
func (r *Runner) sweep(ctx context.Context, kind string) (*tally, error) {
	var run tally

	w, known := r.work[kind]
	if !known {
		return &run, fmt.Errorf("jobs: nothing handles %q", kind)
	}

	due, err := r.store.DueJobs(ctx, kind, w.Policy.Batch)
	if err != nil {
		// Ours, not a job's: the queue could not be read at all.
		if ctx.Err() == nil {
			r.log.Error("could not read the job queue", "kind", kind, "error", err)
		}
		return &run, err
	}

	r.each(ctx, due, w.Policy.Concurrency, func(job *store.Job) {
		run.add(r.run(ctx, w, job))
	})
	return &run, nil
}

// each runs fn over the jobs, at most width at a time, and returns when they are all finished.
//
// Bounded rather than one goroutine per job: width is how much of a crowd this looks like from
// the other end, and it is the only thing standing between a hundred queued pictures and a
// hundred simultaneous requests.
func (r *Runner) each(ctx context.Context, due []*store.Job, width int, fn func(*store.Job)) {
	if width <= 1 {
		for _, job := range due {
			// Checked between jobs rather than only at the top: a shutdown arriving
			// mid-sweep should stop the sweep, and every job here is an outbound request
			// that can take a while.
			if ctx.Err() != nil {
				return
			}
			fn(job)
		}
		return
	}

	queue := make(chan *store.Job)
	var wg sync.WaitGroup
	for range width {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				fn(job)
			}
		}()
	}
	for _, job := range due {
		if ctx.Err() != nil {
			break
		}
		queue <- job
	}
	close(queue)
	wg.Wait()
}

// run does one job and records what happened.
//
// Three lines, the same three for every kind of work there will ever be: it started, and then
// it either ended or failed. Started is the one that looks redundant and is not — a job that
// hangs leaves a "started" with nothing after it, and that gap is the only evidence there is.
// The label is what makes them readable: the queue holds an opaque payload and a ULID, and
// neither answers "which feed?".
//
// Duration is on both endings, because these are requests to other people's servers under a
// timeout, and "the queue is slow" and "one host is slow" look identical in a count.
func (r *Runner) run(ctx context.Context, w Work, job *store.Job) Outcome {
	// Everything a line about this job carries. The attempt number is here rather than only
	// on failures, because "this succeeded on its third go" is the interesting kind of
	// success.
	at := []any{"kind", job.Kind, "job", job.ID}
	if job.Label != "" {
		at = append(at, "label", job.Label)
	}
	if job.Attempts > 0 {
		at = append(at, "attempt", job.Attempts+1)
	}

	r.log.Debug("job started", at...)

	started := time.Now()
	err := w.Handle(ctx, job.Payload)
	at = append(at, "took", time.Since(started).Round(time.Millisecond))

	switch {
	case err == nil:
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear a finished job", "job", job.ID, "error", err)
			return Later
		}
		r.log.Debug("job ended", at...)
		return Done

	// A cancelled context is this program shutting down, not the job failing. Counting it as
	// an attempt would spend a job's retries on its own restarts, and logging it as a failure
	// would fill a shutdown with things that did not go wrong.
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return Waiting
	}

	// The end of the line, either because the handler said so or because the tries ran out.
	// Info, not Debug: this is a piece of work that is never happening, and somebody wondering
	// why should not have to turn Debug on and restart to find out.
	if errors.Is(err, Drop) || job.Attempts+1 >= w.Policy.MaxAttempts {
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear a failed job", "job", job.ID, "error", err)
		}
		r.log.Info("job failed", append(at, "error", err)...)
		return GaveUp
	}

	wait := w.Policy.wait(job.Attempts)
	if err := r.store.RetryJob(ctx, job.ID, wait, err.Error()); err != nil {
		r.log.Error("could not reschedule a job", "job", job.ID, "error", err)
	}
	// Debug, because it is coming back: a failure that will be retried is not yet news.
	r.log.Debug("job failed", append(at, "again_in", wait, "error", err)...)
	return Later
}

// ReportInterval is how often a kind that is still working says so.
//
// A minute. The summary used to be written only when a queue went quiet, which reads fine on
// paper and is useless in practice: pictures run one job every three seconds on purpose, so two
// hundred of them is ten minutes during which a log at the default level says nothing at all.
// Ten minutes of silence and "it is not running" look identical, and telling those two apart is
// the whole reason to log a background queue.
const ReportInterval = time.Minute

// Run works every registered kind until ctx is cancelled, and returns when they have stopped.
func (r *Runner) Run(ctx context.Context) {
	// One line to say the runner exists, beside "databases open" and "listening".
	//
	// Without it a restart with nothing to do prints nothing at all, and an idle queue is
	// indistinguishable from a queue that never started — which is exactly the question
	// somebody restarting a server is asking. The depth is what answers it: nothing waiting is
	// an empty queue, and something waiting that never falls is a stuck one.
	kinds := make([]string, 0, len(r.work))
	for kind := range r.work {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	if depth, err := r.store.QueueDepth(ctx, ""); err == nil {
		// Naming the kinds rather than counting them, because this line replaced one per
		// background worker and it should still answer the question those answered: is the
		// thing I am waiting on actually running in this build?
		r.log.Info("job queue open", "waiting", depth, "kinds", kinds)
	}
	r.warnAboutStrangers(ctx)

	var wg sync.WaitGroup
	for kind, w := range r.work {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.work1(ctx, kind, w)
		}()
	}
	wg.Wait()
}

// warnAboutStrangers says so if the queue holds work nothing here can run.
//
// This is what a job enqueued by a newer version and read by an older one looks like — a
// downgrade, or a half-finished deploy. The rows are left alone rather than dropped, since
// throwing them away would be losing work; but nothing sweeps them, so without this line the
// only symptom is a number in "waiting" that never goes down.
//
// Once, at startup, rather than per job: it is a fact about the build, and it cannot change
// while the process is running.
func (r *Runner) warnAboutStrangers(ctx context.Context) {
	kinds, err := r.store.QueuedKinds(ctx)
	if err != nil {
		return
	}
	var strangers []string
	for _, kind := range kinds {
		if _, known := r.work[kind]; !known {
			strangers = append(strangers, kind)
		}
	}
	if len(strangers) > 0 {
		slices.Sort(strangers)
		r.log.Warn("the queue holds work nothing here can run",
			"kinds", strangers, "note", "queued by a newer version; left in place")
	}
}

// work1 is one kind's loop: look for work, do what is due, ask for more when there is none.
//
// One goroutine per kind, and one sweep at a time within it. That is what makes claiming
// unnecessary — no row can be picked up twice, because nothing else is looking at this kind —
// and it keeps the table a queue rather than a lock manager. A sweep that overruns its tick
// simply starts the next one late, which is the right answer: the pace is a ceiling on how fast
// to go, not a promise to keep up.
func (r *Runner) work1(ctx context.Context, kind string, w Work) {
	ticker := time.NewTicker(w.Policy.Every)
	defer ticker.Stop()

	var run tally
	// Dry to begin with, and both times zero, so the first tick asks for work rather than
	// waiting a minute to find what was already in the table when this started, and so a long
	// drain reports a minute in rather than a minute after it finished.
	dry := true
	var lastRefill, lastReport time.Time

	for {
		select {
		case <-ctx.Done():
			// A shutdown mid-run should still say what the run did, or a container that
			// restarts often is a container whose queue is invisible.
			r.report(ctx, "worked the queue", kind, &run)
			return
		case <-ticker.C:
		}

		if w.Refill != nil && dry && time.Since(lastRefill) >= w.Policy.RefillEvery {
			lastRefill = time.Now()
			if n, err := w.Refill(ctx); err != nil {
				if ctx.Err() == nil {
					r.log.Error("could not look for more work", "kind", kind, "error", err)
				}
			} else if n > 0 {
				r.log.Debug("queued more work", "kind", kind, "count", n)
			}
		}

		swept, err := r.sweep(ctx, kind)
		dry = err != nil || !swept.any()
		if !dry {
			run.merge(swept)
			// Still going. Said once a minute rather than once a job — two hundred pictures
			// should read as a few sentences, not as two hundred of them, and not as nothing
			// until it is over.
			//
			// The clock starts on the first sweep of a run rather than at zero. Starting it at
			// zero reported immediately and then again when the queue went quiet, so a burst
			// that finished inside one sweep — which is every feed cycle — printed the same
			// counts twice.
			if lastReport.IsZero() {
				lastReport = time.Now()
			} else if time.Since(lastReport) >= ReportInterval {
				lastReport = time.Now()
				r.report(ctx, "working the queue", kind, &run)
			}
			continue
		}
		// Quiet. Past tense, and the last word on this run.
		r.report(ctx, "worked the queue", kind, &run)
		run = tally{}
		lastReport = time.Time{}
	}
}

// report says where a run of one kind's queue has got to.
func (r *Runner) report(ctx context.Context, msg, kind string, run *tally) {
	if !run.any() {
		return
	}

	run.mu.Lock()
	at := []any{"kind", kind, "done", run.done, "gave_up", run.gaveUp}
	if run.later > 0 {
		at = append(at, "queued_again", run.later)
	}
	run.mu.Unlock()

	if depth, err := r.store.QueueDepth(ctx, kind); err == nil {
		at = append(at, "waiting", depth)
	}

	// Info rather than Debug. This is the line that answers "is the queue moving, and is
	// anything getting through?", and somebody asking that should not have to turn Debug on
	// and restart to find out. Counting what gave up beside what worked is the point: forty
	// done and none given up is a different instance from forty done and two hundred given up,
	// and a bare "done" hides the difference. Counting what is still waiting is what makes it
	// progress rather than a status.
	r.log.Info(msg, at...)
}

// Enqueue adds work for a kind this runner knows about.
//
// Checked, because a job enqueued for a kind nobody handles sits in the table until somebody
// notices — and the moment to notice is the moment it is written, not a day later in a log.
func (r *Runner) Enqueue(ctx context.Context, kind, key, label, payload string) error {
	if _, known := r.work[kind]; !known {
		return fmt.Errorf("jobs: nothing handles %q", kind)
	}
	return r.store.Enqueue(ctx, kind, key, label, payload)
}
