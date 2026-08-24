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

// Sweep works through what is due, and reports how many it finished.
func (r *Runner) Sweep(ctx context.Context) int {
	due, err := r.store.DueJobs(ctx, PerTick)
	if err != nil {
		r.log.Error("could not read the job queue", "error", err)
		return 0
	}

	done := 0
	for _, job := range due {
		// Checked between jobs rather than only at the top: a shutdown arriving mid-sweep
		// should stop the sweep, and every job here is an outbound request that can take a
		// while.
		if ctx.Err() != nil {
			break
		}
		if r.run(ctx, job) {
			done++
		}
	}
	return done
}

// run does one job and records what happened. Reports whether it is finished with.
func (r *Runner) run(ctx context.Context, job *store.Job) bool {
	handler, known := r.handlers[job.Kind]
	if !known {
		// A kind nobody handles. Left in place rather than dropped: this is what a job
		// enqueued by a newer version and read by an older one looks like, and throwing that
		// away would be losing work on a downgrade.
		r.log.Warn("no handler for a queued job", "kind", job.Kind, "job", job.ID)
		if err := r.store.RetryJob(ctx, job.ID, 24*time.Hour, "no handler for "+job.Kind); err != nil {
			r.log.Error("could not defer a job", "job", job.ID, "error", err)
		}
		return false
	}

	err := handler(ctx, job.Payload)
	switch {
	case err == nil:
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear a finished job", "job", job.ID, "error", err)
			return false
		}
		return true

	case errors.Is(err, Drop):
		r.log.Debug("dropped a job", "kind", job.Kind, "job", job.ID, "reason", err)
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear a dropped job", "job", job.ID, "error", err)
		}
		return true

	// A cancelled context is this program shutting down, not the job failing. Counting it as
	// an attempt would spend a job's retries on its own restarts.
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return false
	}

	if job.Attempts+1 >= MaxAttempts {
		r.log.Info("giving up on a job", "kind", job.Kind, "job", job.ID,
			"attempts", job.Attempts+1, "error", err)
		if err := r.store.FinishJob(ctx, job.ID); err != nil {
			r.log.Error("could not clear an exhausted job", "job", job.ID, "error", err)
		}
		return true
	}

	wait := Backoff[min(job.Attempts, len(Backoff)-1)]
	if err := r.store.RetryJob(ctx, job.ID, wait, err.Error()); err != nil {
		r.log.Error("could not reschedule a job", "job", job.ID, "error", err)
	}
	return false
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

// Run works the queue until ctx is cancelled.
//
// before, when set, runs at the start of each pass — for topping the queue up from whatever
// state the rest of the program has got into, so that work is never lost merely because
// nothing thought to enqueue it.
func (r *Runner) Run(ctx context.Context, before func(context.Context)) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	worked := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.Sweep(ctx) > 0 {
				worked++
				continue
			}
			// Nothing was due. That is the moment to look for more work, rather than every
			// three seconds regardless — an idle queue should not mean a database query per
			// tick for the life of the process.
			if worked > 0 {
				depth, _ := r.store.QueueDepth(ctx)
				r.log.Debug("worked the queue", "done", worked, "waiting", depth)
				worked = 0
			}
			if before != nil {
				before(ctx)
			}
		}
	}
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
