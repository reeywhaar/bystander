// Package jobs runs the background work that is not on a schedule of its own.
//
// Fetching a feed happens every so many minutes and is over in a second. Everything else that
// happens away from a request shares a different shape: it is about one thing, it can fail, it
// should be tried again a few times and then left alone, and it must survive a restart. That
// is a queue, and this is the smallest one that is honestly a queue.
//
// There is no worker pool and no locking. One runner, one job at a time, a handful per tick —
// because the work is outbound requests to other people's servers and the constraint is
// politeness, not throughput. A queue that drains as fast as it can is a queue that gets a
// reader's address blocked.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// Backoff is how long to wait after each failed attempt.
//
// Ten minutes, an hour, a day. The first failure is usually something passing — a timeout, a
// rate limit that has already reset — and by the third it is usually the answer.
var Backoff = []time.Duration{10 * time.Minute, time.Hour, 24 * time.Hour}

// MaxAttempts is how many times a job is tried before it is dropped.
const MaxAttempts = 3

// PerTick is how many jobs one pass will run.
//
// One. Together with [Interval] that is the whole rate limit: at most one outbound request
// every few seconds, no matter how much work is waiting.
//
// A handful per pass would be less total traffic and worse behaviour. Work arrives in bursts —
// a feed with thirty new articles is thirty pictures, and they all live on one host — so a
// pass that ran eight of them would make eight requests to the same server inside a second.
// Bursts are what a host rate-limits; a steady trickle at a lower peak is both politer and
// less likely to be refused.
const PerTick = 1

// Runner holds the handlers and runs what is due.
type Runner struct {
	store *store.Store
	log   *slog.Logger

	handlers map[string]Handler
}

func New(st *store.Store, log *slog.Logger) *Runner {
	return &Runner{store: st, log: log, handlers: map[string]Handler{}}
}

// Handle registers the handler for a kind of work.
//
// Panics on a second registration for the same kind, because that is two pieces of code each
// believing they own it and the wrong one would win silently.
func (r *Runner) Handle(kind string, fn Handler) {
	if _, taken := r.handlers[kind]; taken {
		panic("jobs: two handlers registered for " + kind)
	}
	r.handlers[kind] = fn
}

// Outcome is what became of one job.
type Outcome int

const (
	// Waiting means nothing ran: the queue was empty, or the sweep was cut short.
	Waiting Outcome = iota
	// Done means the handler succeeded and the job is gone.
	Done
	// GaveUp means the job will not be tried again — the handler said so, or it ran out of
	// attempts. Not an error: for pictures it is the ordinary answer from a host that would
	// rather not be asked.
	GaveUp
	// Later means it failed and is queued for another go.
	Later
)

// tally counts what a run of the queue did, so the summary can say more than "some".
type tally struct{ done, gaveUp, later int }

func (t *tally) add(o Outcome) {
	switch o {
	case Done:
		t.done++
	case GaveUp:
		t.gaveUp++
	case Later:
		t.later++
	}
}

func (t tally) any() bool { return t.done+t.gaveUp+t.later > 0 }

// Sweep works through what is due, and reports how many jobs it dealt with for good.
func (r *Runner) Sweep(ctx context.Context) (Outcome, error) {
	due, err := r.store.DueJobs(ctx, PerTick)
	if err != nil {
		// Ours, not a job's: the queue could not be read at all.
		r.log.Error("could not read the job queue", "error", err)
		return Waiting, err
	}
	if len(due) == 0 {
		return Waiting, nil
	}

	last := Waiting
	for _, job := range due {
		// Checked between jobs rather than only at the top: a shutdown arriving mid-sweep
		// should stop the sweep, and every job here is an outbound request that can take a
		// while.
		if ctx.Err() != nil {
			break
		}
		last = r.run(ctx, job)
	}
	return last, nil
}

// run does one job and records what happened.
//
// Every job leaves a line at Debug — what it was, how long it took, and how it ended. Duration
// is the reason: these are requests to other people's servers under a timeout, and "the queue
// is slow" and "one host is slow" look identical in a count.
func (r *Runner) run(ctx context.Context, job *store.Job) Outcome {
	handler, known := r.handlers[job.Kind]
	if !known {
		// A kind nobody handles. Left in place rather than dropped: this is what a job
		// enqueued by a newer version and read by an older one looks like, and throwing that
		// away would be losing work on a downgrade.
		// Warn, not Debug: a job nothing can run is a queue that will not drain, and the
		// only way anybody finds out is here.
		r.log.Warn("no handler for a queued job", "kind", job.Kind, "job", job.ID)
		if err := r.store.RetryJob(ctx, job.ID, 24*time.Hour, "no handler for "+job.Kind); err != nil {
			r.log.Error("could not defer a job", "job", job.ID, "error", err)
		}
		return Later
	}

	started := time.Now()
	err := handler(ctx, job.Payload)
	took := time.Since(started).Round(time.Millisecond)

	// Everything a line about one job carries. The attempt number is here rather than only on
	// failures, because "this succeeded on its third go" is the interesting kind of success.
	at := []any{"kind", job.Kind, "job", job.ID, "took", took}
	if job.Attempts > 0 {
		at = append(at, "attempt", job.Attempts+1)
	}

	switch {
	case err == nil:
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear a finished job", "job", job.ID, "error", err)
			return Later
		}
		r.log.Debug("job done", at...)
		return Done

	case errors.Is(err, Drop):
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear a dropped job", "job", job.ID, "error", err)
		}
		r.log.Debug("job gave up", append(at, "reason", err)...)
		return GaveUp

	// A cancelled context is this program shutting down, not the job failing. Counting it as
	// an attempt would spend a job's retries on its own restarts, and logging it as a failure
	// would fill a shutdown with things that did not go wrong.
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return Waiting
	}

	if job.Attempts+1 >= MaxAttempts {
		// Info: this is the end of the line for a piece of work, and somebody wondering why
		// it never happened should find it without turning Debug on.
		r.log.Info("job exhausted its attempts", append(at, "error", err)...)
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear an exhausted job", "job", job.ID, "error", err)
		}
		return GaveUp
	}

	wait := Backoff[min(job.Attempts, len(Backoff)-1)]
	if err := r.store.RetryJob(ctx, job.ID, wait, err.Error()); err != nil {
		r.log.Error("could not reschedule a job", "job", job.ID, "error", err)
	}
	r.log.Debug("job failed, queued again", append(at, "in", wait, "error", err)...)
	return Later
}

// Interval is how often the queue is looked at, and so how far apart two outbound requests
// can possibly be made.
//
// Three seconds, which drains a feed's worth of pictures over a couple of minutes and would
// take an hour to make the number of requests a browser makes opening one news site. Nobody
// is waiting on any of it: the page is complete before a single job runs.
//
// A ticker rather than each job scheduling the next one when it finishes. Self-scheduling
// gives the same spacing — it measures from the result, so a slow request cannot stack — but
// the chain has no owner: lose the process between finishing a job and queuing the next, and
// the work simply stops, with nothing to notice. A ticker starts again on its own every time.
const Interval = 3 * time.Second

// RefillInterval is how often the runner looks for work nobody has queued yet.
//
// A minute, and only when the queue is empty. That is slow enough that an idle instance is
// idle and quick enough that nothing waits meaningfully — nobody is watching for a picture to
// be measured.
const RefillInterval = time.Minute

// Run works the queue until ctx is cancelled.
//
// refill, when set, is asked for more work whenever the queue is empty — at startup, and at
// most once a [RefillInterval] after that.
//
// One mechanism rather than a hook wherever work might appear. That was the first design and
// it had three of them: after a poll, after the hourly sweep, at startup. It still missed the
// commonest case, because adding a feed through the interface saves its articles directly and
// never touches the poller — so a new feed's pictures waited an hour for the sweep to notice
// them. Asking "is there anything to do?" cannot be forgotten by a code path that did not know
// it was supposed to say so, and it covers the abrupt restart for free: whatever is in the
// table is picked up on the next tick regardless of who put it there.
func (r *Runner) Run(ctx context.Context, refill func(context.Context)) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	var run tally
	// Zero, so the first empty tick refills rather than waiting a minute to find work that
	// was already in the table when this started.
	var lastRefill time.Time

	for {
		select {
		case <-ctx.Done():
			// A shutdown mid-run should still say what the run did, or a container that
			// restarts often is a container whose queue is invisible.
			r.report(ctx, run)
			return
		case <-ticker.C:
			outcome, err := r.Sweep(ctx)
			if err == nil && outcome != Waiting {
				run.add(outcome)
				continue
			}
			// The queue has gone quiet. One line for the whole run rather than one per job:
			// a hundred pictures should read as a sentence, not as a hundred sentences.
			r.report(ctx, run)
			run = tally{}

			if refill != nil && time.Since(lastRefill) >= RefillInterval {
				lastRefill = time.Now()
				refill(ctx)
			}
		}
	}
}

// report says what a run of the queue came to, once it has gone quiet.
func (r *Runner) report(ctx context.Context, run tally) {
	if !run.any() {
		return
	}

	depth, err := r.store.QueueDepth(ctx)
	at := []any{"done", run.done, "gave_up", run.gaveUp}
	if run.later > 0 {
		at = append(at, "queued_again", run.later)
	}
	if err == nil {
		at = append(at, "waiting", depth)
	}

	// Info rather than Debug. This is one line per drained queue — a handful a day — and it
	// is the line that answers "is the queue moving, and is anything getting through?".
	// Counting what gave up beside what worked is the point: forty done and none given up is
	// a different instance from forty done and two hundred given up, and a bare "done" hides
	// the difference.
	r.log.Info("worked the queue", at...)
}

// Enqueue adds work for a kind this runner knows about.
//
// Checked, because a job enqueued for a kind nobody handles sits in the table until somebody
// notices — and the moment to notice is the moment it is written, not a day later in a log.
func (r *Runner) Enqueue(ctx context.Context, kind, key, payload string) error {
	if _, known := r.handlers[kind]; !known {
		return fmt.Errorf("jobs: nothing handles %q", kind)
	}
	return r.store.Enqueue(ctx, kind, key, payload)
}
