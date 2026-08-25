package feeds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bystander/internal/jobs"
	"bystander/internal/store"
)

// A store on disk, because this is about what gets written to it.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// A picture is measured from its header, so the test serves one and counts the bytes that
// actually leave. That number is the whole claim this file makes.
func TestMeasuringReadsTheHeaderAndNotThePicture(t *testing.T) {
	// Noise, not a gradient: PNG squeezed a smooth one down to twenty kilobytes, which is
	// too small to tell "read the header" from "read the file".
	img := image.NewRGBA(image.Rect(0, 0, 1200, 900))
	noise := rand.New(rand.NewPCG(1, 2))
	for y := range 900 {
		for x := range 1200 {
			img.Set(x, y, color.RGBA{
				uint8(noise.UintN(256)), uint8(noise.UintN(256)), uint8(noise.UintN(256)), 0xff,
			})
		}
	}
	var whole bytes.Buffer
	if err := png.Encode(&whole, img); err != nil {
		t.Fatal(err)
	}
	if whole.Len() < 64<<10 {
		t.Fatalf("the test picture is only %d bytes; it cannot show that the rest is skipped", whole.Len())
	}

	var served int
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Honouring Range is the polite path, and the one most hosts take.
		if rng := r.Header.Get("Range"); strings.HasPrefix(rng, "bytes=0-") {
			w.WriteHeader(http.StatusPartialContent)
			n, _ := w.Write(whole.Bytes()[:16<<10])
			served += n
			return
		}
		n, _ := w.Write(whole.Bytes())
		served += n
	}))
	defer host.Close()

	st := testStore(t)
	feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	item := &store.Item{
		ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One",
		Link: "https://example.com/1", ImageURL: host.URL + "/pic.png",
		// Dated, or it falls outside every window and Queues hands back nothing.
		PublishedAt: time.Now().Add(-time.Hour), FetchedAt: time.Now(),
	}
	if _, err := st.SaveItems(t.Context(), []*store.Item{item}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]string{"url": item.ImageURL})
	if err := Measure(st, "test")(t.Context(), string(payload)); err != nil {
		t.Fatalf("Measure(): %v", err)
	}

	got, err := st.Queues(t.Context(), "", "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[feed.ID].Fresh) != 1 {
		t.Fatalf("%d articles", len(got[feed.ID].Fresh))
	}
	if w, h := got[feed.ID].Fresh[0].ImageWidth, got[feed.ID].Fresh[0].ImageHeight; w != 1200 || h != 900 {
		t.Errorf("measured %dx%d, want 1200x900", w, h)
	}

	// The point of the whole exercise: a fraction of the file crossed the wire.
	if served >= whole.Len() {
		t.Errorf("served %d bytes of a %d byte picture — the header is supposed to be enough",
			served, whole.Len())
	}
	t.Logf("read %d bytes of %d", served, whole.Len())
}

func TestMeasuringGivesUpOnAnswersThatWillNotChange(t *testing.T) {
	st := testStore(t)

	for _, code := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusGone} {
		host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		payload, _ := json.Marshal(map[string]string{"url": host.URL + "/pic.png"})
		err := Measure(st, "test")(t.Context(), string(payload))
		host.Close()

		// Asking again is asking somebody's server to keep saying no on a timer.
		if !errors.Is(err, jobs.Drop) {
			t.Errorf("a %d is retried: %v", code, err)
		}
	}
}

// Nothing is retried, so every answer that is not a picture ends the same way.
//
// A 429 and a 404 differ in what they mean and not at all in what to do about them: the page
// draws a shape for an unmeasured picture and looks exactly as it does now, so no answer here
// is worth a second request to somebody else's server.
func TestMeasuringDoesNotComeBackAfterATemporaryRefusal(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		st := testStore(t)
		feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
		if err != nil {
			t.Fatal(err)
		}

		host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		now := time.Now()
		if _, err := st.SaveItems(t.Context(), []*store.Item{{
			ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
			ImageURL: host.URL + "/pic.png", PublishedAt: now.Add(-time.Hour), FetchedAt: now,
		}}); err != nil {
			t.Fatal(err)
		}

		payload, _ := json.Marshal(map[string]string{"url": host.URL + "/pic.png"})
		err = Measure(st, "test")(t.Context(), string(payload))
		host.Close()

		if !errors.Is(err, jobs.Drop) {
			t.Errorf("a %d is queued for another go: %v", code, err)
		}
		// And the picture is marked, so the next top-up does not offer it again — which is
		// the loop that made the queue impolite in the first place.
		if urls, err := st.UnmeasuredImages(t.Context(), 10); err != nil || len(urls) != 0 {
			t.Errorf("after a %d the picture is queued again: %v (%v)", code, urls, err)
		}
	}
}

func TestMeasuringGivesUpOnThingsThatAreNotPictures(t *testing.T) {
	st := testStore(t)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A soft 404, or an SVG, or an AVIF — none of which becomes readable by being
		// fetched a second time.
		w.Write([]byte("<!doctype html><title>not a picture</title>"))
	}))
	defer host.Close()

	payload, _ := json.Marshal(map[string]string{"url": host.URL + "/pic.png"})
	if err := Measure(st, "test")(t.Context(), string(payload)); !errors.Is(err, jobs.Drop) {
		t.Errorf("err = %v, want it dropped", err)
	}
}

// One measurement answers for every article sharing the picture, which is what keeps a
// publisher's standing illustration from being asked about once per article.
func TestOneMeasurementAnswersForEveryArticleSharingThePicture(t *testing.T) {
	st := testStore(t)
	feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}

	const shared = "https://example.com/banner.png"
	now := time.Now()
	items := []*store.Item{
		{ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
			ImageURL: shared, PublishedAt: now.Add(-time.Hour), FetchedAt: now},
		{ID: "a_2", FeedID: feed.ID, GUID: "g2", Title: "Two", Link: "https://example.com/2",
			ImageURL: shared, PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
	}
	if _, err := st.SaveItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}

	// One job, not two.
	urls, err := st.UnmeasuredImages(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Fatalf("%d jobs for one picture: %v", len(urls), urls)
	}

	if err := st.SetImageSize(t.Context(), shared, 800, 600); err != nil {
		t.Fatal(err)
	}
	got, err := st.Queues(t.Context(), "", "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Asserted, because a loop over nothing passes without saying anything.
	if len(got[feed.ID].Fresh) != 2 {
		t.Fatalf("%d articles back, want the two that were saved", len(got[feed.ID].Fresh))
	}
	for _, item := range got[feed.ID].Fresh {
		if item.ImageWidth != 800 || item.ImageHeight != 600 {
			t.Errorf("%s is %dx%d", item.ID, item.ImageWidth, item.ImageHeight)
		}
	}

	// And nothing is left to ask about.
	if urls, err := st.UnmeasuredImages(t.Context(), 10); err != nil || len(urls) != 0 {
		t.Errorf("still queued: %v (%v)", urls, err)
	}
}

// A picture nothing can measure must stop being asked about.
func TestAPictureThatFailedIsLeftAloneAndThenAskedAgain(t *testing.T) {
	st := testStore(t)
	feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := st.SaveItems(t.Context(), []*store.Item{{
		ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
		ImageURL: "https://example.com/gone.png", PublishedAt: now.Add(-time.Hour), FetchedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}

	// The job runs, gets a settled answer, and is put out of reach for the day.
	if err := st.PostponeImage(t.Context(), "https://example.com/gone.png", Gone, MeasureRetryLater); err != nil {
		t.Fatalf("PostponeImage(): %v", err)
	}

	// The queue must not offer it again on the next top-up.
	urls, err := st.UnmeasuredImages(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 0 {
		t.Errorf("a picture that was given up on is queued again: %v", urls)
	}

	// But it is offered again a day later, which is the whole difference between "not now"
	// and "not ever". This test used to stop at the line above, and the thing it was
	// guarding turned out to be a trap: any failure — one timeout, one rate limit, one bad
	// minute at a CDN — cost that picture its size for as long as the article existed.
	//
	// Fifteen of the nineteen pictures on a real comics page were stuck this way, and when
	// they were replayed by hand against the same hosts with the same headers, every one of
	// them measured on the first try.
	st.SetClock(func() time.Time { return time.Now().Add(MeasureRetryLater + time.Minute) })

	urls, err = st.UnmeasuredImages(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Errorf("a picture left alone for a day is still not offered again: %v", urls)
	}
}

// A picture that answered is never asked again, however long ago it answered.
//
// The retry is keyed on the size still being missing rather than on how long ago the asking
// happened — otherwise every measured picture on the instance would come back round once a
// day, which is the same rudeness the queue exists to avoid, at the scale of the whole
// database.
func TestAMeasuredPictureIsNeverAskedAgain(t *testing.T) {
	st := testStore(t)
	feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if _, err := st.SaveItems(t.Context(), []*store.Item{{
		ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
		ImageURL: "https://example.com/measured.png", PublishedAt: now.Add(-time.Hour), FetchedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetImageSize(t.Context(), "https://example.com/measured.png", 800, 600); err != nil {
		t.Fatalf("SetImageSize(): %v", err)
	}

	st.SetClock(func() time.Time { return time.Now().Add(30 * 24 * time.Hour) })

	if urls, err := st.UnmeasuredImages(t.Context(), 10); err != nil || len(urls) != 0 {
		t.Errorf("a measured picture came back round: %v (%v)", urls, err)
	}
}

// Widening how far back a feed reaches must not turn up articles with unmeasured pictures.
//
// The two things are decided in different places and it is not obvious they agree. A feed's
// window is applied when a page is composed — Queues bounds what it will
// offer — and nowhere else. Nothing filters by it on the way in, so an article older than the
// window is saved like any other, and it is kept for the retention counted from when it was
// *fetched* rather than when it was published.
//
// So the pool a widened window reaches into is already in the database, and the question is
// whether the measuring worker has been looking at it. It has, because UnmeasuredImages asks
// only whether a picture has been probed and never how old its article is. This is the test
// that says so: widen the window afterwards and every article that appears is one whose
// picture was measured long before anybody asked for it.
func TestWideningAFeedsReachFindsPicturesAlreadyMeasured(t *testing.T) {
	st := testStore(t)
	feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}

	// A feed carrying a year of articles, all fetched now — a first fetch of an archive, or a
	// feed that publishes its whole back catalogue.
	now := time.Now()
	ages := []time.Duration{
		time.Hour,
		3 * 24 * time.Hour,
		20 * 24 * time.Hour,
		90 * 24 * time.Hour,
		300 * 24 * time.Hour,
	}
	items := make([]*store.Item, len(ages))
	for i, age := range ages {
		items[i] = &store.Item{
			FeedID:      feed.ID,
			GUID:        fmt.Sprintf("g%d", i),
			Title:       fmt.Sprintf("Story %d", i),
			Link:        fmt.Sprintf("https://example.com/%d", i),
			ImageURL:    fmt.Sprintf("https://example.com/%d.png", i),
			PublishedAt: now.Add(-age),
			FetchedAt:   now,
		}
	}
	if _, err := st.SaveItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}

	// Every picture is asked about, including the ones on articles no window would offer yet.
	urls, err := st.UnmeasuredImages(t.Context(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != len(ages) {
		t.Fatalf("%d pictures to measure, want all %d — the worker only looks at recent ones",
			len(urls), len(ages))
	}
	for _, url := range urls {
		if err := st.SetImageSize(t.Context(), url, 800, 600); err != nil {
			t.Fatal(err)
		}
	}

	// A week's reach offers the recent ones.
	week := map[string]time.Time{feed.ID: now.Add(-7 * 24 * time.Hour)}
	narrow, err := st.Queues(t.Context(), "pg_1", "p_1", []string{feed.ID}, 50, week)
	if err != nil {
		t.Fatal(err)
	}
	if len(narrow[feed.ID].Fresh) != 2 {
		t.Fatalf("a week's reach offers %d articles, want 2", len(narrow[feed.ID].Fresh))
	}

	// Widened to a year it offers all of them, and every one arrives measured.
	year := map[string]time.Time{feed.ID: now.Add(-365 * 24 * time.Hour)}
	wide, err := st.Queues(t.Context(), "pg_1", "p_1", []string{feed.ID}, 50, year)
	if err != nil {
		t.Fatal(err)
	}
	if len(wide[feed.ID].Fresh) != len(ages) {
		t.Fatalf("a year's reach offers %d articles, want %d", len(wide[feed.ID].Fresh), len(ages))
	}
	for _, item := range wide[feed.ID].Fresh {
		if item.ImageWidth == 0 || item.ImageHeight == 0 {
			t.Errorf("%q was published %s ago and its picture is still unmeasured",
				item.Title, now.Sub(item.PublishedAt).Round(24*time.Hour))
		}
	}

	// And nothing is left over, so widening again asks for nothing.
	if left, err := st.UnmeasuredImages(t.Context(), 50); err != nil || len(left) != 0 {
		t.Errorf("still unmeasured: %v (%v)", left, err)
	}
}

// A host having a bad minute is asked again within the hour, not the next day.
//
// The two failures are not the same kind of thing. A 503 or a 429 is a host saying "not now" —
// it was under load, or it thinks we are asking too often — and an hour later it very likely
// answers. A 404 is a settled answer, and asking again within the hour is the same answer at
// the host's expense.
func TestAHostInTroubleIsAskedAgainSoonerThanOneThatSaidNo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header string
		want   time.Duration
		reason string
	}{
		{"a server error", http.StatusServiceUnavailable, "", MeasureRetrySoon, Busy},
		{"too many requests", http.StatusTooManyRequests, "", MeasureRetrySoon, Busy},
		{"gone", http.StatusNotFound, "", MeasureRetryLater, Gone},
		{"refused", http.StatusForbidden, "", MeasureRetryLater, Refused},
		// A host that troubles to say when it will be ready is worth not annoying.
		{"asked for ten minutes", http.StatusTooManyRequests, "600", 10 * time.Minute, Busy},
		// Bounded at both ends: a zero would mean asking straight back, and a week is longer
		// than the article stays interesting.
		{"asked for no time at all", http.StatusTooManyRequests, "0", time.Minute, Busy},
		{"asked for a week", http.StatusServiceUnavailable, "604800", MeasureRetryLater, Busy},
		{"asked in gibberish", http.StatusServiceUnavailable, "soon", MeasureRetrySoon, Busy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(tc.status)
			}))
			defer host.Close()

			st := testStore(t)
			now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
			st.SetClock(func() time.Time { return now })

			feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.SaveItems(t.Context(), []*store.Item{{
				ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
				ImageURL: host.URL + "/pic.png", PublishedAt: now.Add(-time.Hour), FetchedAt: now,
			}}); err != nil {
				t.Fatal(err)
			}

			payload, _ := json.Marshal(map[string]string{"url": host.URL + "/pic.png"})
			if err := Measure(st, "bystander/test")(t.Context(), string(payload)); err == nil {
				t.Fatal("a refusal was reported as a measurement")
			}

			// Why, and not just when — because that is what a later version acts on: add a
			// decoder for a format there was none for, and only the pictures that failed
			// for that reason need asking again.
			if got := reasonFor(t, st, host.URL+"/pic.png"); got != tc.reason {
				t.Errorf("recorded %q, want %q", got, tc.reason)
			}

			// Not yet: a moment before it is due, the queue still holds it back.
			st.SetClock(func() time.Time { return now.Add(tc.want - time.Minute) })
			if urls, err := st.UnmeasuredImages(t.Context(), 10); err != nil || len(urls) != 0 {
				t.Errorf("offered again after %s, want to wait %s", tc.want-time.Minute, tc.want)
			}

			// And then it is.
			st.SetClock(func() time.Time { return now.Add(tc.want + time.Minute) })
			if urls, err := st.UnmeasuredImages(t.Context(), 10); err != nil || len(urls) != 1 {
				t.Errorf("still not offered after %s, want asking again: %v", tc.want+time.Minute, urls)
			}
		})
	}
}

// WebP, which the standard library cannot read and half the web now serves.
//
// Two of the four pictures that genuinely could not be measured on a real instance were WebP —
// one from a stock CDN, one from an art magazine. Both are 640x640 comics' neighbours on the
// same page, and both measured the moment a decoder was registered.
func TestAWebPPictureIsMeasured(t *testing.T) {
	// A minimal lossy WebP: a RIFF container, a VP8 chunk, and a keyframe header saying 16x16.
	pic := []byte{
		'R', 'I', 'F', 'F', 0x1a, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', ' ', 0x0e, 0, 0, 0,
		0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x10, 0x00, 0x10, 0x00, 0, 0, 0, 0,
	}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		w.Write(pic)
	}))
	defer host.Close()

	st := testStore(t)
	feed, err := st.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Example", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := st.SaveItems(t.Context(), []*store.Item{{
		ID: "a_1", FeedID: feed.ID, GUID: "g1", Title: "One", Link: "https://example.com/1",
		ImageURL: host.URL + "/pic.webp", PublishedAt: now.Add(-time.Hour), FetchedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]string{"url": host.URL + "/pic.webp"})
	if err := Measure(st, "bystander/test")(t.Context(), string(payload)); err != nil {
		t.Fatalf("Measure(): %v", err)
	}

	// Read back the way the page would: an article's id is derived from its feed and guid,
	// so the one handed to SaveItems is not the one it is stored under.
	got, err := st.Queues(t.Context(), "", "", []string{feed.ID}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[feed.ID].Fresh) != 1 {
		t.Fatalf("%d articles back, want the one that was saved", len(got[feed.ID].Fresh))
	}
	if item := got[feed.ID].Fresh[0]; item.ImageWidth != 16 || item.ImageHeight != 16 {
		t.Errorf("measured %dx%d, want 16x16 — the WebP decoder is not registered",
			item.ImageWidth, item.ImageHeight)
	}
}

// reasonFor is the category recorded against a picture that could not be measured.
func reasonFor(t *testing.T, st *store.Store, url string) string {
	t.Helper()
	got, err := st.ImageFailures(t.Context(), 10)
	if err != nil {
		t.Fatalf("ImageFailures(): %v", err)
	}
	return got[url]
}
