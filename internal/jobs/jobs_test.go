package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"bystander/internal/store"
)

func testRunner(t *testing.T) (*Runner, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil))), st
}

// depth is how much work is left, which is what most of these assertions come down to.
func depth(t *testing.T, st *store.Store) int {
	t.Helper()
	n, err := st.QueueDepth(t.Context())
	if err != nil {
		t.Fatalf("QueueDepth(): %v", err)
	}
	return n
}

func TestAJobThatSucceedsIsGone(t *testing.T) {
	r, st := testRunner(t)

	var got string
	r.Handle("greet", func(_ context.Context, payload string) error {
		got = payload
		return nil
	})
	if err := r.Enqueue(t.Context(), "greet", "once", "hello"); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != Done {
		t.Errorf("outcome = %v, want Done", outcome)
	}
	if got != "hello" {
		t.Errorf("the handler saw %q, want %q", got, "hello")
	}
	if left := depth(t, st); left != 0 {
		t.Errorf("%d jobs left, want 0", left)
	}
}

// The whole point of Drop: a 404 is an answer, not bad luck, and asking again is asking
// somebody else's server to keep saying no on a schedule.
func TestADroppedJobIsNotTriedAgain(t *testing.T) {
	r, st := testRunner(t)

	tries := 0
	r.Handle("gone", func(context.Context, string) error {
		tries++
		return fmt.Errorf("404: %w", Drop)
	})
	if err := r.Enqueue(t.Context(), "gone", "once", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != GaveUp {
		t.Errorf("outcome = %v, want GaveUp", outcome)
	}
	if left := depth(t, st); left != 0 {
		t.Errorf("%d jobs left, want 0", left)
	}

	if outcome, _ := r.Sweep(t.Context()); outcome != Waiting {
		t.Errorf("a second sweep found something to do; the drop did not take")
	}
	if tries != 1 {
		t.Errorf("the handler ran %d times, want 1", tries)
	}
}

func TestAFailedJobWaitsOutItsBackoffAndThenGivesUp(t *testing.T) {
	r, st := testRunner(t)

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })

	tries := 0
	r.Handle("flaky", func(context.Context, string) error {
		tries++
		return errors.New("the host is having a moment")
	})
	if err := r.Enqueue(t.Context(), "flaky", "once", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	for attempt := 1; attempt < MaxAttempts; attempt++ {
		outcome, err := r.Sweep(t.Context())
		if err != nil {
			t.Fatalf("Sweep(): %v", err)
		}
		if outcome != Later {
			t.Fatalf("attempt %d: outcome = %v, want Later", attempt, outcome)
		}

		// Still there, and not due. A queue that retried immediately would spend all three
		// attempts inside a second, on a host that is probably still having its moment.
		if left := depth(t, st); left != 1 {
			t.Fatalf("attempt %d: %d jobs left, want 1", attempt, left)
		}
		if outcome, _ := r.Sweep(t.Context()); outcome != Waiting {
			t.Fatalf("attempt %d: the job was due again straight away", attempt)
		}
		if tries != attempt {
			t.Fatalf("attempt %d: the handler ran %d times", attempt, tries)
		}

		now = now.Add(Backoff[attempt-1])
	}

	outcome, err := r.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != GaveUp {
		t.Errorf("outcome = %v, want GaveUp on the last attempt", outcome)
	}
	if tries != MaxAttempts {
		t.Errorf("the handler ran %d times, want %d", tries, MaxAttempts)
	}
	if left := depth(t, st); left != 0 {
		t.Errorf("%d jobs left after the attempts ran out, want 0", left)
	}
}

// A job enqueued by a newer version and read by an older one. Losing it would mean a
// downgrade quietly throwing work away, so it is deferred rather than dropped.
func TestAJobNothingHandlesIsKeptForLater(t *testing.T) {
	r, st := testRunner(t)

	// Past the runner, because Enqueue refuses an unknown kind on purpose — the moment to
	// notice is when it is written.
	if err := st.Enqueue(t.Context(), "from.the.future", "once", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != Later {
		t.Errorf("outcome = %v, want Later", outcome)
	}
	if left := depth(t, st); left != 1 {
		t.Errorf("%d jobs left, want the job kept", left)
	}
}

func TestEnqueueRefusesAKindNothingHandles(t *testing.T) {
	r, _ := testRunner(t)
	if err := r.Enqueue(t.Context(), "nobody.handles.this", "once", ""); err == nil {
		t.Error("Enqueue() accepted a kind with no handler")
	}
}

// Shutting down is not the job failing. Counting it as an attempt would spend a job's
// retries on this program's own restarts.
func TestShuttingDownDoesNotCostAJobAnAttempt(t *testing.T) {
	r, st := testRunner(t)

	ctx, cancel := context.WithCancel(t.Context())
	r.Handle("slow", func(ctx context.Context, _ string) error {
		cancel()
		return ctx.Err()
	})
	if err := r.Enqueue(t.Context(), "slow", "once", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != Waiting {
		t.Errorf("outcome = %v, want Waiting", outcome)
	}

	due, err := st.DueJobs(t.Context(), 1)
	if err != nil {
		t.Fatalf("DueJobs(): %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("%d jobs due, want the job still there", len(due))
	}
	if due[0].Attempts != 0 {
		t.Errorf("attempts = %d, want 0: a shutdown is not a failed attempt", due[0].Attempts)
	}
}

// The same work asked for twice is one job. A picture used by a dozen articles is one
// measurement, and a feed refetched every hour must not reset a job that is waiting out its
// backoff.
func TestTheSameWorkQueuedTwiceIsOneJob(t *testing.T) {
	r, st := testRunner(t)
	r.Handle("measure", func(context.Context, string) error { return nil })

	for range 3 {
		if err := r.Enqueue(t.Context(), "measure", "https://example.com/a.jpg", ""); err != nil {
			t.Fatalf("Enqueue(): %v", err)
		}
	}
	if left := depth(t, st); left != 1 {
		t.Errorf("%d jobs queued, want 1", left)
	}
}

func TestARetryingJobIsNotResetByBeingQueuedAgain(t *testing.T) {
	r, st := testRunner(t)

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })

	r.Handle("flaky", func(context.Context, string) error { return errors.New("not now") })
	if err := r.Enqueue(t.Context(), "flaky", "once", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}
	if _, err := r.Sweep(t.Context()); err != nil {
		t.Fatalf("Sweep(): %v", err)
	}

	if err := r.Enqueue(t.Context(), "flaky", "once", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}
	if outcome, _ := r.Sweep(t.Context()); outcome != Waiting {
		t.Error("queueing it again made a backing-off job due; a feed refetched hourly would retry a dead host hourly")
	}
}

// The reason Run takes a refill at all: work can reach the table without anybody telling the
// runner, and the commonest way it does is a restart.
func TestRunAsksForWorkBeforeItsFirstTick(t *testing.T) {
	r, st := testRunner(t)

	ran := make(chan struct{})
	var once sync.Once
	r.Handle("measure", func(context.Context, string) error { return nil })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx, func(ctx context.Context) {
			// What the daemon's refill does: look for work nobody queued.
			if err := r.Enqueue(ctx, "measure", "found-by-asking", ""); err != nil {
				return
			}
			once.Do(func() { close(ran) })
		})
	}()

	select {
	case <-ran:
	case <-ctx.Done():
		t.Fatal("Run never asked for work; nothing would pick up a queue left by a restart")
	}

	// And it is actually worked, not merely written.
	deadline := time.After(20 * time.Second)
	for depth(t, st) > 0 {
		select {
		case <-deadline:
			t.Fatal("the refilled job was never run")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-done
}
