package feeds

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bystander/internal/jobs"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// rss builds a feed publishing one item a day, ending today.
func rss(now time.Time, items int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>The Example</title><link>https://example.com</link>`)
	for i := range items {
		at := now.AddDate(0, 0, -i).Format(time.RFC1123Z)
		fmt.Fprintf(&b, `<item><title>Day %d</title><link>https://example.com/%d</link><guid>%d</guid><pubDate>%s</pubDate></item>`, i, i, i, at)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

// A fetch that brings articles works out when to come back, and says so in the feed.
//
// This is the whole of what the job is for beyond the request itself, and it used to live in a
// poller of its own. One item a day is a median gap of a day, and the schedule should land a
// day out rather than at whatever fixed interval an operator once guessed.
func TestAFetchSchedulesItselfFromWhatWasPublished(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	// Pinned, because the assertion is about an exact schedule and the store keeps times to
	// the second.
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		io.WriteString(w, rss(now, 8))
	}))
	defer host.Close()

	feed, err := st.UpsertFeed(ctx, host.URL, "", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if err := fetchOne(ctx, st, NewFetcher("http://read.example.com"), quiet(), feed); err != nil {
		t.Fatalf("fetchOne(): %v", err)
	}

	after, err := st.FeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("FeedByID(): %v", err)
	}
	if after.FetchInterval != 24*time.Hour {
		t.Errorf("interval = %s, want 24h for a feed publishing once a day", after.FetchInterval)
	}
	// And the interval is actually applied, rather than computed and dropped — which is how
	// every feed came to sit at a flat half hour with an interval of zero.
	if want := now.Add(24 * time.Hour); !after.NextFetchAt.Equal(want) {
		t.Errorf("next fetch %s, want %s", after.NextFetchAt, want)
	}
}

// A feed that will not answer backs off in the feeds table, which is the only schedule there
// is: the queue is told not to retry this kind, so a failure that did not land here would be a
// feed nothing ever asked for again.
func TestAFeedThatWillNotAnswerBacksOffInTheFeedsTable(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	now := st.Now()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "go away", http.StatusServiceUnavailable)
	}))
	defer host.Close()

	feed, err := st.UpsertFeed(ctx, host.URL, "", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if err := fetchOne(ctx, st, NewFetcher("http://read.example.com"), quiet(), feed); err == nil {
		t.Fatal("fetchOne() said a 503 was fine")
	}

	after, err := st.FeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("FeedByID(): %v", err)
	}
	if after.FailureCount != 1 {
		t.Errorf("failures = %d, want 1", after.FailureCount)
	}
	if !after.NextFetchAt.After(now) {
		t.Errorf("next fetch %s is not in the future; nothing else reschedules a failed feed", after.NextFetchAt)
	}
	// The half that is worth reading: what the server actually said.
	if !strings.Contains(after.LastErrorBody, "go away") {
		t.Errorf("error body = %q, want what the server said", after.LastErrorBody)
	}
}

// Only feeds that are actually due are queued, and each of them once.
func TestOnlyDueFeedsAreQueued(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	runner := jobs.New(st, quiet())
	runner.Handle(FetchFeed, jobs.Work{Handle: func(context.Context, string) error { return nil }})

	due, err := st.UpsertFeed(ctx, "https://example.com/due.xml", "Due", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	later, err := st.UpsertFeed(ctx, "https://example.com/later.xml", "Not Yet", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	// Fetched just now and not wanted again for a day.
	if err := st.RecordSuccess(ctx, later.ID, "Not Yet", "https://example.com",
		"", "", 200, 24*time.Hour, st.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("RecordSuccess(): %v", err)
	}

	n, err := QueueDueFeeds(ctx, st, runner, 100)
	if err != nil {
		t.Fatalf("QueueDueFeeds(): %v", err)
	}
	if n != 1 {
		t.Fatalf("queued %d feeds, want only the due one", n)
	}

	queued, err := st.DueJobs(ctx, FetchFeed, 10)
	if err != nil {
		t.Fatalf("DueJobs(): %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("%d jobs queued, want 1", len(queued))
	}
	if !strings.Contains(queued[0].Payload, due.ID) {
		t.Errorf("payload = %q, want the due feed", queued[0].Payload)
	}
	// Named, so a log line and a queue screen say "Due" rather than a ULID.
	if queued[0].Label != "Due" {
		t.Errorf("label = %q, want the feed's title", queued[0].Label)
	}

	// Asked again before anything has run: still one job, not two.
	if _, err := QueueDueFeeds(ctx, st, runner, 100); err != nil {
		t.Fatalf("QueueDueFeeds(): %v", err)
	}
	if left, _ := st.QueueDepth(ctx, FetchFeed); left != 1 {
		t.Errorf("%d jobs queued, want the same one", left)
	}
}

// A feed unfollowed between being queued and being run is not an error to retry.
func TestFetchingAFeedThatIsGoneIsDropped(t *testing.T) {
	st := testStore(t)
	handle := Fetch(st, NewFetcher("http://read.example.com"), quiet())

	err := handle(context.Background(), `{"feed_id":"nope"}`)
	if err == nil {
		t.Fatal("fetching a feed that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), jobs.Drop.Error()) {
		t.Errorf("error = %v, want it dropped rather than retried", err)
	}
}
