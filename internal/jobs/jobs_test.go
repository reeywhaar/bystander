package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
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
	n, err := st.QueueDepth(t.Context(), "")
	if err != nil {
		t.Fatalf("QueueDepth(): %v", err)
	}
	return n
}

func TestAJobThatSucceedsIsGone(t *testing.T) {
	r, st := testRunner(t)

	var got string
	r.Handle("greet", Work{Handle: func(_ context.Context, payload string) error {
		got = payload
		return nil
	}})
	if err := r.Enqueue(t.Context(), "greet", "once", "a greeting", "hello"); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(t.Context(), "greet")
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
	r.Handle("gone", Work{Handle: func(context.Context, string) error {
		tries++
		return fmt.Errorf("404: %w", Drop)
	}})
	if err := r.Enqueue(t.Context(), "gone", "once", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(t.Context(), "gone")
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != GaveUp {
		t.Errorf("outcome = %v, want GaveUp", outcome)
	}
	if left := depth(t, st); left != 0 {
		t.Errorf("%d jobs left, want 0", left)
	}

	if outcome, _ := r.Sweep(t.Context(), "gone"); outcome != Waiting {
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
	r.Handle("flaky", Work{Handle: func(context.Context, string) error {
		tries++
		return errors.New("the host is having a moment")
	}})
	if err := r.Enqueue(t.Context(), "flaky", "once", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	for attempt := 1; attempt < Default.MaxAttempts; attempt++ {
		outcome, err := r.Sweep(t.Context(), "flaky")
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
		if outcome, _ := r.Sweep(t.Context(), "flaky"); outcome != Waiting {
			t.Fatalf("attempt %d: the job was due again straight away", attempt)
		}
		if tries != attempt {
			t.Fatalf("attempt %d: the handler ran %d times", attempt, tries)
		}

		now = now.Add(Default.Backoff[attempt-1])
	}

	outcome, err := r.Sweep(t.Context(), "flaky")
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != GaveUp {
		t.Errorf("outcome = %v, want GaveUp on the last attempt", outcome)
	}
	if tries != Default.MaxAttempts {
		t.Errorf("the handler ran %d times, want %d", tries, Default.MaxAttempts)
	}
	if left := depth(t, st); left != 0 {
		t.Errorf("%d jobs left after the attempts ran out, want 0", left)
	}
}

// A job enqueued by a newer version and read by an older one. Losing it would mean a
// downgrade quietly throwing work away, so it is left in place — and said out loud, because a
// row nothing sweeps is otherwise just a number in "waiting" that never goes down.
func TestAJobNothingHandlesIsKeptAndNoticed(t *testing.T) {
	r, st := testRunner(t)
	r.Handle("measure", Work{Handle: func(context.Context, string) error { return nil }})

	// Past the runner, because Enqueue refuses an unknown kind on purpose — the moment to
	// notice is when it is written.
	if err := st.Enqueue(t.Context(), "from.the.future", "once", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	// Nothing works it: a kind with no registration has no loop, and Sweep says so rather
	// than inventing a policy for work it knows nothing about.
	if _, err := r.Sweep(t.Context(), "from.the.future"); err == nil {
		t.Error("Sweep() worked a kind nothing handles")
	}
	if left := depth(t, st); left != 1 {
		t.Errorf("%d jobs left, want the job kept", left)
	}

	// And it is findable, which is what the warning at startup is built on.
	kinds, err := st.QueuedKinds(t.Context())
	if err != nil {
		t.Fatalf("QueuedKinds(): %v", err)
	}
	if !slices.Contains(kinds, "from.the.future") {
		t.Errorf("QueuedKinds() = %v, want the unknown kind so startup can warn about it", kinds)
	}
}

func TestEnqueueRefusesAKindNothingHandles(t *testing.T) {
	r, _ := testRunner(t)
	if err := r.Enqueue(t.Context(), "nobody.handles.this", "once", "", ""); err == nil {
		t.Error("Enqueue() accepted a kind with no handler")
	}
}

// Shutting down is not the job failing. Counting it as an attempt would spend a job's
// retries on this program's own restarts.
func TestShuttingDownDoesNotCostAJobAnAttempt(t *testing.T) {
	r, st := testRunner(t)

	ctx, cancel := context.WithCancel(t.Context())
	r.Handle("slow", Work{Handle: func(ctx context.Context, _ string) error {
		cancel()
		return ctx.Err()
	}})
	if err := r.Enqueue(t.Context(), "slow", "once", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	outcome, err := r.Sweep(ctx, "slow")
	if err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
	if outcome != Waiting {
		t.Errorf("outcome = %v, want Waiting", outcome)
	}

	due, err := st.DueJobs(t.Context(), "slow", 1)
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
	r.Handle("measure", Work{Handle: func(context.Context, string) error { return nil }})

	for range 3 {
		if err := r.Enqueue(t.Context(), "measure", "https://example.com/a.jpg", "", ""); err != nil {
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

	r.Handle("flaky", Work{Handle: func(context.Context, string) error { return errors.New("not now") }})
	if err := r.Enqueue(t.Context(), "flaky", "once", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}
	if _, err := r.Sweep(t.Context(), "flaky"); err != nil {
		t.Fatalf("Sweep(): %v", err)
	}

	if err := r.Enqueue(t.Context(), "flaky", "once", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}
	if outcome, _ := r.Sweep(t.Context(), "flaky"); outcome != Waiting {
		t.Error("queueing it again made a backing-off job due; a feed refetched hourly would retry a dead host hourly")
	}
}

// The reason a kind can have a Refill at all: work reaches the table without anybody telling
// the runner, and the commonest way it does is a restart.
func TestRunAsksForWorkBeforeItsFirstTick(t *testing.T) {
	r, st := testRunner(t)

	ran := make(chan struct{})
	var once sync.Once
	r.Handle("measure", Work{
		Handle: func(context.Context, string) error { return nil },
		Refill: func(ctx context.Context) (int, error) {
			// What the daemon's refill does: look for work nobody queued.
			if err := r.Enqueue(ctx, "measure", "found-by-asking", "a picture", ""); err != nil {
				return 0, err
			}
			once.Do(func() { close(ran) })
			return 1, nil
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
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

// The reason the queue has a policy per kind rather than one pace for everything.
//
// Measuring pictures is deliberately one at a time and deliberately slow — it is a rate limit,
// not a bottleneck to be fixed. Fetching feeds is neither. Sharing one queue meant a hundred
// queued pictures sat in front of a feed that had come due, and the feed waited five minutes
// for work that nobody was waiting on. Each kind now has its own loop, so a slow one cannot
// hold up a quick one.
func TestASlowKindDoesNotHoldUpAQuickOne(t *testing.T) {
	r, _ := testRunner(t)

	// Stuck rather than slow, which is the same thing to whatever is behind it and easier to
	// be sure about than a sleep.
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	quick := make(chan struct{}, 1)

	fast := Policy{Every: 10 * time.Millisecond, RefillEvery: 10 * time.Millisecond}

	r.Handle("slow", Work{
		Policy: fast,
		Handle: func(ctx context.Context, _ string) error {
			select {
			case <-stuck:
			case <-ctx.Done():
			}
			return nil
		},
		Refill: func(ctx context.Context) (int, error) {
			return 1, r.Enqueue(ctx, "slow", "slow/1", "the stuck one", "")
		},
	})
	r.Handle("quick", Work{
		Policy: fast,
		Handle: func(context.Context, string) error {
			select {
			case quick <- struct{}{}:
			default:
			}
			return nil
		},
		Refill: func(ctx context.Context) (int, error) {
			return 1, r.Enqueue(ctx, "quick", "quick/1", "the quick one", "")
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()

	select {
	case <-quick:
	case <-ctx.Done():
		t.Fatal("a jammed kind stopped every other kind; that is the single queue this replaced")
	}
	cancel()
	<-done
}

// A kind that says one attempt gets one attempt.
//
// Feeds are the case: next_fetch_at is already a schedule, worked out from how often that
// publisher writes and backed off when they stop answering. A second retry clock in the queue
// would disagree with it, and the disagreement would be invisible — two mechanisms both
// convinced they own when a feed is next asked.
func TestAKindThatSaysOneAttemptDoesNotRetry(t *testing.T) {
	r, st := testRunner(t)

	tries := 0
	r.Handle("fetch", Work{
		Policy: Policy{MaxAttempts: 1},
		Handle: func(context.Context, string) error {
			tries++
			return errors.New("nothing answered")
		},
	})
	if err := r.Enqueue(t.Context(), "fetch", "once", "The Example", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	if outcome, err := r.Sweep(t.Context(), "fetch"); err != nil || outcome != GaveUp {
		t.Fatalf("Sweep() = %v, %v; want GaveUp", outcome, err)
	}
	if tries != 1 {
		t.Errorf("the handler ran %d times, want 1", tries)
	}
	if left := depth(t, st); left != 0 {
		t.Errorf("%d jobs left, want the job finished rather than queued against its own schedule", left)
	}
}

// Concurrency is per kind and it is real: six feeds fetch at once, one picture at a time.
func TestAWideKindRunsItsJobsTogether(t *testing.T) {
	r, _ := testRunner(t)

	const width = 4
	together := make(chan struct{}, width)
	all := make(chan struct{})
	var once sync.Once

	r.Handle("wide", Work{
		Policy: Policy{Batch: width, Concurrency: width},
		Handle: func(ctx context.Context, _ string) error {
			together <- struct{}{}
			if len(together) == width {
				once.Do(func() { close(all) })
			}
			// Held until every one of them has arrived, which cannot happen unless they
			// really are running at the same time.
			select {
			case <-all:
			case <-ctx.Done():
			}
			return nil
		},
	})
	for i := range width {
		if err := r.Enqueue(t.Context(), "wide", fmt.Sprintf("job/%d", i), "", ""); err != nil {
			t.Fatalf("Enqueue(): %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	swept := make(chan error, 1)
	go func() { _, err := r.Sweep(ctx, "wide"); swept <- err }()

	select {
	case <-all:
	case <-ctx.Done():
		t.Fatalf("only %d of %d jobs ran at once", len(together), width)
	}
	if err := <-swept; err != nil {
		t.Fatalf("Sweep(): %v", err)
	}
}

// The label is the only readable thing about a job: the payload is opaque on purpose and the
// id is a ULID. Every log line and every future queue screen is built on it.
func TestAJobRemembersWhatItIsAbout(t *testing.T) {
	r, st := testRunner(t)
	r.Handle("fetch", Work{Handle: func(context.Context, string) error { return nil }})

	if err := r.Enqueue(t.Context(), "fetch", "fetch f-123", "Poorly Drawn Lines", `{"feed_id":"f-123"}`); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	due, err := st.DueJobs(t.Context(), "fetch", 1)
	if err != nil {
		t.Fatalf("DueJobs(): %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("%d jobs due, want 1", len(due))
	}
	if due[0].Label != "Poorly Drawn Lines" {
		t.Errorf("label = %q, want the feed's name — a ULID answers nobody's question", due[0].Label)
	}
}

// One kind's queue must not be measured by another's. The report line says "waiting", and with
// two hundred pictures queued a feed sweep reporting two hundred waiting would be nonsense.
func TestQueueDepthCountsOneKindAtATime(t *testing.T) {
	r, st := testRunner(t)
	r.Handle("a", Work{Handle: func(context.Context, string) error { return nil }})
	r.Handle("b", Work{Handle: func(context.Context, string) error { return nil }})

	for i := range 3 {
		if err := r.Enqueue(t.Context(), "a", fmt.Sprintf("a/%d", i), "", ""); err != nil {
			t.Fatalf("Enqueue(): %v", err)
		}
	}
	if err := r.Enqueue(t.Context(), "b", "b/0", "", ""); err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	for kind, want := range map[string]int{"a": 3, "b": 1, "": 4} {
		got, err := st.QueueDepth(t.Context(), kind)
		if err != nil {
			t.Fatalf("QueueDepth(%q): %v", kind, err)
		}
		if got != want {
			t.Errorf("QueueDepth(%q) = %d, want %d", kind, got, want)
		}
	}
}

// A sweep that does nineteen jobs says nineteen.
//
// It said one. A sweep used to be a single job — pictures take one at a time — so the summary
// was built from one outcome per sweep and nobody noticed the difference until feeds started
// taking a hundred. "done=1" after fetching nineteen feeds is the kind of number somebody reads
// as a queue that is barely moving.
func TestTheSummaryCountsEveryJobInASweep(t *testing.T) {
	r, _ := testRunner(t)

	const n = 19
	r.Handle("fetch", Work{
		Policy: Policy{Batch: n, Concurrency: 6},
		Handle: func(context.Context, string) error { return nil },
	})
	for i := range n {
		if err := r.Enqueue(t.Context(), "fetch", fmt.Sprintf("feed/%d", i), "", ""); err != nil {
			t.Fatalf("Enqueue(): %v", err)
		}
	}

	swept, err := r.sweep(t.Context(), "fetch")
	if err != nil {
		t.Fatalf("sweep(): %v", err)
	}
	if swept.done != n {
		t.Errorf("done = %d, want %d — the summary is the number somebody judges the queue by", swept.done, n)
	}
}
