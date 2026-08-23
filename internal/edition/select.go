// Package edition composes a front page: which articles appear on it, where they sit, and
// when the next one is due.
//
// The sampler in this file is pure. It takes buckets, weights and a seed and returns
// placements; it opens no transaction, reads no clock and touches no store. That is what
// makes it testable against a fixed seed rather than against a database, and it is why the
// interesting part of this program can be reasoned about without one.
//
// The argument for the algorithm — why probability rather than ordering, why a per-feed
// cap, why the tag hierarchy takes no part — is in private/docs/edition.md.
package edition

import (
	"math/rand/v2"

	"bystander/internal/store"
)

// Source is one feed as the sampler sees it: a weight and a queue of candidates, newest
// first.
type Source struct {
	FeedID   string
	Priority int
	Items    []*store.Item
}

// Bucket is a tag together with the feeds tagged with it.
//
// The untagged bucket is an ordinary bucket with an empty TagID and the default priority.
// Nothing downstream treats it specially, which is the point: "no tag" is a bucket, not a
// special case threaded through the loop.
type Bucket struct {
	TagID    string
	Priority int
	FeedIDs  []string
}

// Select draws up to size articles.
//
// Two stages per draw — a bucket weighted by tag priority, then a feed within it weighted
// by feed priority — repeated until the page is full or nothing is left to draw from.
// Priority is a probability of being drawn rather than a sort order: a feed at 90 appears
// more often than one at 10 across editions without ever silencing it.
//
// A short result is an honest result. When the pool runs dry the page is shorter, and it
// is never padded with articles already shown or with a reach back through the archive.
func Select(buckets []Bucket, sources map[string]*Source, size int, seed int64) []store.Pick {
	if size <= 0 {
		return nil
	}

	// Weight zero means never. Zero-weight entries are removed here rather than drawn and
	// discarded, so a page of nothing but zeroes terminates instead of spinning.
	pool := make([]Bucket, 0, len(buckets))
	for _, b := range buckets {
		if b.Priority <= 0 {
			continue
		}
		feeds := make([]string, 0, len(b.FeedIDs))
		for _, id := range b.FeedIDs {
			if src := sources[id]; src != nil && src.Priority > 0 && len(src.Items) > 0 {
				feeds = append(feeds, id)
			}
		}
		if len(feeds) > 0 {
			pool = append(pool, Bucket{TagID: b.TagID, Priority: b.Priority, FeedIDs: feeds})
		}
	}

	// Two streams from one seed, so a generation replays exactly.
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>32|1))

	limit := perFeedCap(size, countFeeds(pool))
	taken := make(map[string]int, len(sources)) // feed id -> how many it has contributed
	cursor := make(map[string]int, len(sources))
	picked := make(map[string]bool, size)

	var picks []store.Pick
	for len(picks) < size && len(pool) > 0 {
		bi := weightedIndex(rng, len(pool), func(i int) int { return pool[i].Priority })
		bucket := &pool[bi]

		fi := weightedIndex(rng, len(bucket.FeedIDs), func(i int) int {
			return sources[bucket.FeedIDs[i]].Priority
		})
		feedID := bucket.FeedIDs[fi]
		src := sources[feedID]

		// Advance past anything already taken. A feed reachable from two tags is one
		// queue, so an article drawn through "Art" is not offered again through "News".
		item := (*store.Item)(nil)
		for cursor[feedID] < len(src.Items) {
			candidate := src.Items[cursor[feedID]]
			cursor[feedID]++
			if !picked[candidate.ID] {
				item = candidate
				break
			}
		}

		if item == nil {
			dropFeed(&pool, feedID)
			continue
		}

		picks = append(picks, store.Pick{Item: item, Rank: len(picks)})
		picked[item.ID] = true
		taken[feedID]++

		// The cap is what stops one prolific publisher taking the page when the dice
		// agree with it. A capped feed leaves every bucket, not just this one.
		if taken[feedID] >= limit || cursor[feedID] >= len(src.Items) {
			dropFeed(&pool, feedID)
		}
	}

	assignSlots(picks, size)
	return picks
}

// perFeedCap is how much of a page one feed may be.
//
// A fifth of the page — but never less than the page divided by the number of feeds that
// could fill it. The cap exists to stop one prolific publisher taking the page when there
// is enough variety to avoid it; it is not there to starve somebody who follows three
// feeds. Without the second term, a person with two subscriptions asking for sixty
// articles would be handed twenty-four and no explanation.
//
// Never less than two either, so a page drawn from a single feed is still a page.
func perFeedCap(size, feeds int) int {
	limit := (size + 4) / 5
	if feeds > 0 {
		if even := (size + feeds - 1) / feeds; even > limit {
			limit = even
		}
	}
	if limit < 2 {
		return 2
	}
	return limit
}

// countFeeds is how many distinct feeds the pool can draw from, which is what decides
// whether the cap has any variety to enforce.
func countFeeds(pool []Bucket) int {
	seen := make(map[string]bool)
	for _, b := range pool {
		for _, id := range b.FeedIDs {
			seen[id] = true
		}
	}
	return len(seen)
}

// dropFeed removes a feed from every bucket, and any bucket it empties.
func dropFeed(pool *[]Bucket, feedID string) {
	buckets := *pool
	out := buckets[:0]
	for _, b := range buckets {
		feeds := b.FeedIDs[:0]
		for _, id := range b.FeedIDs {
			if id != feedID {
				feeds = append(feeds, id)
			}
		}
		b.FeedIDs = feeds
		if len(b.FeedIDs) > 0 {
			out = append(out, b)
		}
	}
	*pool = out
}

// weightedIndex picks an index in [0,n) with probability proportional to weight(i).
//
// A linear scan over the cumulative total. n is the number of a person's tags, or the
// feeds in one tag — tens, drawn sixty times a day.
func weightedIndex(rng *rand.Rand, n int, weight func(int) int) int {
	total := 0
	for i := range n {
		total += weight(i)
	}
	if total <= 0 {
		return rng.IntN(n)
	}
	roll := rng.IntN(total)
	for i := range n {
		roll -= weight(i)
		if roll < 0 {
			return i
		}
	}
	return n - 1
}

// assignSlots decides how prominently each article is laid out, by rank.
//
// Done here, at generation time, and stored — so the client renders slots rather than
// computing them, the page does not reflow after paint, and two loads of one edition are
// identical.
func assignSlots(picks []store.Pick, size int) {
	features := size * 8 / 100
	if features < 1 && len(picks) > 1 {
		features = 1
	}

	for i := range picks {
		item := picks[i].Item
		// A card sized for a picture that has no picture is what makes a page look
		// broken. Demoting is cheaper than styling around it, and it happens regardless
		// of rank — including for the lead.
		if item.ImageURL == "" && item.Summary == "" {
			picks[i].Slot = store.SlotBrief
			continue
		}
		switch {
		case i == 0:
			picks[i].Slot = store.SlotLead
		case i <= features:
			picks[i].Slot = store.SlotFeature
		default:
			picks[i].Slot = store.SlotStandard
		}
	}
}
