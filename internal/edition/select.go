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
// # There is no per-feed cap, and none is needed
//
// There was one, on the stated grounds that a prolific publisher should not be able to
// take the page. It cannot. A draw picks a *feed* and then takes one article from it, so a
// feed with five hundred articles is drawn exactly as often as one with five at the same
// priority. Volume buys nothing; only priority does.
//
// The cap was therefore guarding against something the sampler already makes impossible,
// and it was not free: any cap expressed as a fraction of the page flattens the mix
// whenever there are few enough feeds that the caps alone can fill it. At a fifth and the
// default page of sixty, that was everybody following five feeds or fewer — for whom the
// priority sliders did precisely nothing. A feed's share of the page is now its share of
// the weights, which is what the slider says it is.
//
// # When there is not enough fresh
//
// A page with room left over and nothing new to put in it looks broken rather than honest,
// so the remainder is drawn from `seen` — articles this person has already been shown. The
// same weighting decides which, so a page that has to repeat itself repeats the feeds
// somebody said they cared about.
//
// Nothing is invented: an article that was actually read comes back with its read mark
// intact (see store.ReplaceEdition), so it arrives greyed rather than pretending to be new.
// One that was merely shown and never read comes back plain, which is fair — it was never
// read. And when both pools are dry the page really is short, which is still the honest
// answer.
func Select(buckets []Bucket, sources, seen map[string]*Source, size int, seed int64) []store.Pick {
	if size <= 0 {
		return nil
	}

	// Weight zero means never. Zero-weight entries are left out here rather than drawn and
	// discarded, so a page of nothing but zeroes terminates instead of spinning.
	build := func(from map[string]*Source) []Bucket {
		pool := make([]Bucket, 0, len(buckets))
		for _, b := range buckets {
			if b.Priority <= 0 {
				continue
			}
			feeds := make([]string, 0, len(b.FeedIDs))
			for _, id := range b.FeedIDs {
				if src := from[id]; src != nil && src.Priority > 0 && len(src.Items) > 0 {
					feeds = append(feeds, id)
				}
			}
			if len(feeds) > 0 {
				pool = append(pool, Bucket{TagID: b.TagID, Priority: b.Priority, FeedIDs: feeds})
			}
		}
		return pool
	}

	drawing := sources
	pool := build(drawing)
	repeating := false

	// Two streams from one seed, so a generation replays exactly.
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>32|1))

	cursor := make(map[string]int, len(sources))
	picked := make(map[string]bool, size)

	var picks []store.Pick
	for len(picks) < size {
		if len(pool) == 0 {
			// Nothing fresh left. Fall back to what has been seen before rather than
			// handing somebody two thirds of a page and no explanation.
			if repeating || len(seen) == 0 {
				break
			}
			repeating = true
			drawing = seen
			clear(cursor)
			if pool = build(drawing); len(pool) == 0 {
				break
			}
			continue
		}

		bi := weightedIndex(rng, len(pool), func(i int) int { return pool[i].Priority })
		bucket := &pool[bi]

		fi := weightedIndex(rng, len(bucket.FeedIDs), func(i int) int {
			return drawing[bucket.FeedIDs[i]].Priority
		})
		feedID := bucket.FeedIDs[fi]
		src := drawing[feedID]

		// Advance past anything already taken. A feed reachable from two tags is one
		// queue, so an article drawn through "Art" is not offered again through "News".
		var item *store.Item
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

		if cursor[feedID] >= len(src.Items) {
			dropFeed(&pool, feedID)
		}
	}

	assignSlots(picks, rng)
	return picks
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

// wideSlots are the widths worth more than one column, widest first.
//
// The page is sixteen tracks: lead takes all of them, wide twelve, feature eight, and an
// ordinary column four. Every one is a multiple of the narrowest, so a row always has room
// for something — but they do not all tile against each other, and twelve is the one that
// does not. A row holding a twelve has four tracks left, which only a column can fill, so
// `grid-auto-flow: dense` has to reach past the next article to find one. That reaching is
// the whole mechanism: a page that backfills is a page with a shape instead of rows.
var wideSlots = []store.Slot{store.SlotLead, store.SlotWide, store.SlotFeature}

// openerWeights and bodyWeights are how often each width is drawn, in the same order.
//
// The opener is drawn evenly: the page should not begin the same shape every time, and all
// three of these are a reasonable way to begin.
//
// Below it, full width is deliberately rare. One story running the whole page halfway down is
// a landmark somebody can navigate by; three of them is a page chopped into bands, and the
// thing they were supposed to stand out from is gone.
var (
	// Evenly: the page should not begin the same shape every time, and all three of these
	// are a reasonable way to begin.
	openerWeights = []int{1, 1, 1}
	bodyWeights   = []int{1, 2, 3}
)

// drawSlot picks a width by weight.
func drawSlot(rng *rand.Rand, weights []int) store.Slot {
	total := 0
	for _, w := range weights {
		total += w
	}
	roll := rng.IntN(total)
	for i, w := range weights {
		roll -= w
		if roll < 0 {
			return wideSlots[i]
		}
	}
	return wideSlots[len(wideSlots)-1]
}

// assignSlots decides how wide each article is laid out, and how prominently.
//
// Done here, at generation time, and stored — so the client renders slots rather than
// computing them, the page does not reflow after paint, and two loads of one edition are
// identical. That last part is what the whole thing is for: a page somebody can come back to
// and find something in again. See web/src/lib/voice.ts.
//
// Two rules shape it, and they pull in opposite directions on purpose.
//
// **The page opens with weight.** The first card is never a single column. A front page that
// began with four narrow ones would have nothing to look at first, and every paper ever
// printed leads with something big. Which of the three wide slots it gets is drawn, so the
// top of the page is not the same shape every time.
//
// **After that, width is scattered rather than spent.** The old rule handed the widest slots
// to ranks one through four and left everything below identical, so the page ran big to small
// and then stayed small for forty cards. Rank here is draw order out of a weighted sample, not
// an editor's judgement of importance, so there is nothing to preserve by stacking prominence
// at the top — and a page with a full-width story halfway down reads as a page rather than as
// a list that has been sorted.
func assignSlots(picks []store.Pick, rng *rand.Rand) {
	// A page can come out empty — everything read, nothing left in the pool — and the rules
	// below all start from "the first card", which there then is not.
	if len(picks) == 0 {
		return
	}

	// Roughly one card in four gets more than its column, the first one included.
	//
	// One in eight was the first attempt and it was too thin: on a page of twenty-eight that
	// is three wide cards and twenty-five identical quarters, which reads as a uniform page
	// with a couple of accidents in it rather than as a page that was laid out. The variation
	// has to be common enough that a reader stops expecting the next card to look like the
	// last one — that is the whole mechanism by which any of it becomes a landmark.
	//
	// Not much more than a quarter, though. If half the page is wide then wide is the norm
	// and the quarters become the exception, which is the same problem wearing the other hat.
	wides := max(len(picks)/4, 1)

	for i := range picks {
		item := picks[i].Item
		// A card sized for a picture that has no picture is what makes a page look
		// broken. Demoting is cheaper than styling around it, and it happens regardless
		// of rank — including for the lead.
		if item.ImageURL == "" && item.Summary == "" {
			picks[i].Slot = store.SlotBrief
			continue
		}
		picks[i].Slot = store.SlotStandard
	}

	// Where the wide ones go. The first card always, then one somewhere in each of the
	// remaining bands — which spreads them down the page without letting two land together.
	at := []int{}
	if picks[0].Slot != store.SlotBrief {
		at = append(at, 0)
	}
	if band := (len(picks) - 1) / max(wides, 1); band > 0 {
		for k := len(at); k < wides; k++ {
			// One position per band, drawn inside it. A fixed offset would put every wide
			// card at the same place in its band, which is a pattern a reader picks up
			// well before they could name it.
			//
			// Nothing stops two landing next to each other, and nothing should. Adjacent
			// here means adjacent in reading order, not side by side on the page: `dense`
			// decides that. A ten beside a six is a row that tiles exactly; a ten beside an
			// eight pushes the eight down and pulls a later column up into the gap. Keeping
			// them apart would be guessing at the grid's job and getting it wrong.
			pos := 1 + (k * band) + rng.IntN(band)
			if pos >= len(picks) {
				break
			}
			if picks[pos].Slot != store.SlotStandard {
				continue
			}
			at = append(at, pos)
		}
	}

	// Which width each of them gets is drawn — so the top of the page is not the same shape
	// every time, and a full-width story can turn up halfway down without being the rule.
	for _, pos := range at {
		weights := bodyWeights
		if pos == 0 {
			weights = openerWeights
		}
		picks[pos].Slot = drawSlot(rng, weights)
	}
}
