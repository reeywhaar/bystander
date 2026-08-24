package feeds

import (
	"testing"
	"time"
)

// at builds a run of publishing times, newest last, `gap` apart.
func at(n int, gap time.Duration) []time.Time {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	out := make([]time.Time, n)
	for i := range out {
		out[i] = base.Add(-time.Duration(n-1-i) * gap)
	}
	return out
}

// The configured interval is gone: the rule decides for itself, between these two.
const configured = MinFetchInterval

func TestCadenceFollowsHowOftenAFeedPublishes(t *testing.T) {
	for _, c := range []struct {
		what string
		gap  time.Duration
		n    int
		want time.Duration
	}{
		// A busy feed is held at the configured interval: this only ever makes fetching
		// less frequent than the operator asked for.
		{"a news wire", 13 * time.Minute, 30, MinFetchInterval},
		{"a link aggregator", 35 * time.Minute, 25, 35 * time.Minute},
		{"a design blog", 22 * time.Hour, 10, 22 * time.Hour},
		// And a comic published every three weeks is looked at weekly rather than every
		// half hour, which is the whole point.
		{"a comic", 19 * 24 * time.Hour, 10, MaxFetchInterval},
	} {
		if got := Cadence(at(c.n, c.gap)); got != c.want {
			t.Errorf("%s publishing every %s: %s, want %s", c.what, c.gap, got, c.want)
		}
	}
}

// The bound that matters, and the one the obvious formulation misses. A feed that publishes
// ten items in an hour and then nothing for a month has a median of weeks and a window of one
// hour: wait for the median and every one of those ten is gone before it is ever fetched.
func TestCadenceNeverOutlastsTheFeedsOwnWindow(t *testing.T) {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	// Nine items inside one hour, then one a month later — so the median gap is enormous
	// and the span is not.
	burst := make([]time.Time, 0, 10)
	for i := range 9 {
		burst = append(burst, base.Add(time.Duration(i)*6*time.Minute))
	}
	burst = append(burst, base.Add(30*24*time.Hour))

	got := Cadence(burst)
	span := burst[len(burst)-1].Sub(burst[0])
	if got > span/2 {
		t.Errorf("interval %s is more than half the feed's %s window", got, span)
	}
}

func TestCadenceSaysNothingFromTooLittle(t *testing.T) {
	for _, c := range []struct {
		what  string
		times []time.Time
	}{
		{"an empty feed", nil},
		{"a feed with one article", at(1, time.Hour)},
		{"a feed with two", at(2, time.Hour)},
		// Every article at the same instant, which is what an archive dumped in one go
		// looks like. It says nothing about how often anything is published — and it is
		// real: one of the feeds measured reads as publishing every minute for this reason.
		{"an archive published at once", at(20, 0)},
	} {
		if got := Cadence(c.times); got != UnknownFetchInterval {
			t.Errorf("%s: %s, want a day", c.what, got)
		}
	}
}

// The order a feed lists its items in is the publisher's business, not a fact about timing.
func TestCadenceDoesNotCareWhatOrderItIsGiven(t *testing.T) {
	forward := at(10, 4*time.Hour)
	backward := make([]time.Time, len(forward))
	for i, when := range forward {
		backward[len(forward)-1-i] = when
	}

	if a, b := Cadence(forward), Cadence(backward); a != b {
		t.Errorf("newest-first gave %s and oldest-first gave %s", b, a)
	}
}

// Half an hour is the fastest this ever polls, whatever a feed does.
func TestCadenceNeverPollsFasterThanTheFloor(t *testing.T) {
	for _, gap := range []time.Duration{time.Second, time.Minute, 29 * time.Minute} {
		if got := Cadence(at(30, gap)); got != MinFetchInterval {
			t.Errorf("a feed publishing every %s gave %s, want %s", gap, got, MinFetchInterval)
		}
	}
}

// Too little to judge by is a day — often enough that a new feed finding its feet is noticed
// the same day, rare enough that a nearly empty one is not polled at the busy rate for nothing.
func TestCadenceFallsBackToADay(t *testing.T) {
	if got := Cadence(at(2, time.Hour)); got != 24*time.Hour {
		t.Errorf("two articles gave %s, want a day", got)
	}
}
