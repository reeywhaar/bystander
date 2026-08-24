package feeds

import (
	"sort"
	"time"
)

const (
	// MinFetchInterval is the fastest any feed is polled, however often it publishes.
	//
	// Half an hour. Nothing here is live: a page is composed on a schedule and an article
	// arriving twenty minutes sooner reaches the same page. This used to be configurable and
	// is not any more — an operator setting it had no way to know how often each of their
	// feeds published, which is the only thing that should decide it.
	MinFetchInterval = 30 * time.Minute

	// MaxFetchInterval is the longest a feed is left unfetched, however rarely it publishes.
	//
	// A week. Past that a feed nobody has checked stops being followed in any useful sense —
	// a publisher who comes back after a month should be noticed within days rather than
	// whenever the next article happens to fall due.
	MaxFetchInterval = 7 * 24 * time.Hour

	// UnknownFetchInterval is used when a feed has published too little to judge by.
	//
	// A day: often enough that a new feed finding its feet is noticed the same day, rare
	// enough that a nearly empty one is not polled at the busy rate for nothing. It is a
	// starting point rather than a verdict — three articles is all it takes to replace it
	// with something measured.
	UnknownFetchInterval = 24 * time.Hour
)

// minSample is how many articles it takes to say anything about how often a feed publishes.
//
// Three, which gives two gaps and therefore a median that is one of them rather than an
// average of nothing. Below that the feed is new, nearly empty, or one this program has only
// just met.
const minSample = 3

// Cadence is how long to wait before fetching a feed again, from what it has just published.
//
// The idea is that a comic published every three weeks does not need looking at every half
// hour: measured against nineteen real feeds, following the publishing rate cuts fetches by
// about four fifths, and nearly all of that saving is at the slow end. A weekly comic polled
// every thirty minutes is three hundred and thirty-six requests between articles.
//
// Two things bound it, and the second is the one that is easy to get wrong.
//
// **The floor is [MinFetchInterval].** This only ever makes fetching *less* frequent than it
// already was, never more, so the rule cannot introduce a risk the old fixed interval did not
// already have.
//
// **The ceiling is the feed's own window, halved.** A feed carries a fixed number of items and
// the oldest falls off as new ones arrive; wait longer than that and articles are lost before
// they are ever fetched — permanently, because there is nowhere else to get them. The span
// between the oldest and newest item in hand *is* that window, measured rather than assumed.
// Halved for margin, because a publisher having a busy afternoon shortens it without warning.
//
// Bounding by the median times the item count instead reads as the same idea and is not: that
// is the span again, so it can never bind. It also misses the case it was meant to catch — a
// feed that publishes ten items in an hour and then nothing for a month has a median of weeks
// and a window of one hour.
func Cadence(published []time.Time) time.Duration {
	if len(published) < minSample {
		return UnknownFetchInterval
	}

	at := make([]time.Time, len(published))
	copy(at, published)
	sort.Slice(at, func(i, j int) bool { return at[i].Before(at[j]) })

	gaps := make([]time.Duration, 0, len(at)-1)
	for i := 1; i < len(at); i++ {
		if gap := at[i].Sub(at[i-1]); gap > 0 {
			gaps = append(gaps, gap)
		}
	}
	if len(gaps) == 0 {
		// Every article carries the same timestamp, which is what a feed that published its
		// whole archive at once looks like. It says nothing about how often it publishes —
		// and it is real: one of the feeds this was measured against reads as publishing
		// every minute for exactly this reason.
		return UnknownFetchInterval
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })

	// The median rather than the mean: one archive dump or one three-year-old post drags a
	// mean across the whole range, and both are common in feeds people actually follow.
	interval := gaps[len(gaps)/2]

	if window := at[len(at)-1].Sub(at[0]); window > 0 && interval > window/2 {
		interval = window / 2
	}

	if interval < MinFetchInterval {
		return MinFetchInterval
	}
	if interval > MaxFetchInterval {
		return MaxFetchInterval
	}
	return interval
}
