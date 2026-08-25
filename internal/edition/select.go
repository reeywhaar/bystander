// Package edition composes a front page: which articles appear on it, where they sit, and
// when the next one is due.
//
// The sampler in this file is pure. It takes queues, weights and a seed and returns
// placements; it opens no transaction, reads no clock and touches no store. That is what
// makes it testable against a fixed seed rather than against a database, and it is why the
// interesting part of this program can be reasoned about without one.
//
// The argument for the algorithm — why a feed's share rather than an article's chance, and
// why tags decide only whether a feed is eligible — is in docs/edition.md.
package edition

import (
	"math/rand/v2"
	"slices"

	"bystander/internal/store"
)

// Source is one feed as the sampler sees it: a weight, and its articles in the order this
// page wants them. See store.Queue for what puts them in that order.
type Source struct {
	FeedID   string
	Priority int
	Fresh    []*store.Item
	Unread   []*store.Item
	Read     []*store.Item
}

// Select composes a page of up to size articles.
//
// # A feed's share of the page is its share of the priorities, and nothing else
//
// Each feed is given a quota — `size × priority / Σ priorities` — and the page is filled from
// the front of each feed's queue. Two properties fall out of that, and both are the product.
//
// **Volume buys nothing.** A quota is a number of articles, so a publisher posting two
// hundred times a day is allotted exactly what one posting twice is, at the same priority.
// The alternative — any scheme that picks articles rather than feeds — hands the page to
// whoever writes most: on a real subscription list, shuffling gave two feeds set to 25 and 10
// forty-one places out of ninety, because between them they had a third of the articles.
//
// **Tags take no part.** A tag decides whether a feed is on this page at all, which is
// edition.eligible and the page's own filter lists. It does not weigh anything: a tag
// priority meant a feed carrying three tags was drawn from three buckets and took a quarter
// of the page at the same slider setting as a feed carrying one.
//
// # Quotas rather than draws
//
// A weighted draw repeated until the page is full has the same expectation and much more
// variance: five feeds at equal priority filling thirty places came out 11, 6, 5, 4, 4 —
// lopsided for no reason a reader could name. Quotas give 7, 6, 6, 6, 5. Priority is still a
// share rather than an order, but it is the share it says it is on every page rather than on
// average over a month.
//
// The randomness that is left is in *which* articles fill a quota and in the leftover places
// — see apportion, where it is load-bearing rather than decorative.
//
// # When a feed cannot fill its quota
//
// Its places go back and are apportioned again over the feeds that still have something, and
// again until the page is full or every queue is dry. Without that a page is short whenever
// any feed is thin, which is most pages.
//
// # A band at a time, across every feed
//
// Three passes over the same queues, one per band, and the whole page is apportioned afresh
// in each. Everything new from every feed is placed before anything already seen from any
// feed; every unread repeat before any read one. A pass per band rather than a queue read
// straight through, because otherwise a feed with a large quota and nothing new contributes
// something already read while another feed still has unread articles waiting.
//
// A page with room left over and nothing new to put in it looks broken rather than honest,
// which is what the later passes are for. Nothing is invented: a repeat that was read arrives
// with its read mark, so it is greyed rather than pretending to be new. When all three bands
// are dry the page really is short, which is still the honest answer.
func Select(sources map[string]*Source, size int, seed int64) []store.Pick {
	if size <= 0 {
		return nil
	}

	// Zero means never — a real setting, and how somebody keeps a feed subscribed but out of
	// rotation. Dropped here rather than allotted nothing, so it cannot take a place through
	// a rounding remainder.
	feeds := make([]string, 0, len(sources))
	for id, src := range sources {
		if src != nil && src.Priority > 0 {
			feeds = append(feeds, id)
		}
	}
	// Sorted, because a map's order is not stable and a page that cannot be replayed from
	// its seed is the one thing the seed is for.
	slices.Sort(feeds)

	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>32|1))

	var picks []store.Pick
	taken := make(map[string]bool, size)
	cursor := make(map[string]int, len(feeds))

	for _, band := range []func(*Source) []*store.Item{
		func(s *Source) []*store.Item { return s.Fresh },
		func(s *Source) []*store.Item { return s.Unread },
		func(s *Source) []*store.Item { return s.Read },
	} {
		clear(cursor)
		from := len(picks)

		for len(picks) < size {
			left := make(map[string]int, len(feeds))
			for _, id := range feeds {
				if n := len(band(sources[id])) - cursor[id]; n > 0 {
					left[id] = n
				}
			}
			if len(left) == 0 {
				break
			}

			placed := 0
			for id, quota := range apportion(rng, size-len(picks), feeds, left, sources) {
				items := band(sources[id])
				for ; quota > 0 && cursor[id] < len(items); cursor[id]++ {
					item := items[cursor[id]]
					// A feed reachable more than one way is still one queue, so an article
					// already on the page is stepped over rather than placed twice.
					if taken[item.ID] {
						continue
					}
					taken[item.ID] = true
					picks = append(picks, store.Pick{Item: item})
					quota--
					placed++
				}
			}
			// Every remaining queue held nothing but articles already placed. Apportioning
			// again would allot the same places to the same empty queues, forever.
			if placed == 0 {
				break
			}
		}

		// Interleave the feeds. Each one's quota was taken in a run, so without this a page
		// is one publisher after another in blocks — and since the slots are assigned by
		// position, the lead and both features would come from whichever feed sorted first.
		rest := picks[from:]
		rng.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	}

	for i := range picks {
		picks[i].Rank = i
	}
	assignSlots(picks, rng)
	return picks
}

// apportion divides room places among the feeds that still have something, in proportion to
// their priorities.
//
// Each feed gets the whole part of its share outright. The places left over — there are
// always some, since the shares almost never come out whole — are handed out one at a time by
// a weighted draw on the fractional parts.
//
// **Drawn rather than given to the largest fractions**, and that is the whole reason this is
// not three lines. Largest-remainder is the textbook answer and it silences: a feed whose
// share is 0.4 of a place has the same fractional part on every page, loses every time, and
// never appears at all. Priority is a probability of being drawn, not a sort order — zero
// means never and nothing else does, and a feed at 10 has to turn up occasionally or the
// slider has a dead zone nobody documented. Drawing on the fractions keeps the expectation
// exactly right and lets the small ones through at their own rate.
//
// A feed is never allotted more than it has left. Its unused places are not redistributed
// here; the caller apportions again over what remains, which is the same thing and
// terminates.
func apportion(rng *rand.Rand, room int, order []string, left map[string]int, sources map[string]*Source) map[string]int {
	total := 0
	for id := range left {
		total += sources[id].Priority
	}
	if total == 0 || room <= 0 {
		return nil
	}

	quota := make(map[string]int, len(left))
	spent := 0

	// Built in `order`, not in map order: the draw below consumes randomness in sequence, so
	// an unstable order is a page that cannot be replayed from its seed.
	ids := make([]string, 0, len(left))
	fraction := make([]float64, 0, len(left))
	for _, id := range order {
		if left[id] == 0 {
			continue
		}
		exact := float64(room) * float64(sources[id].Priority) / float64(total)
		whole := min(int(exact), left[id])
		quota[id] = whole
		spent += whole
		ids = append(ids, id)
		fraction = append(fraction, exact-float64(int(exact)))
	}

	for spent < room {
		// Only feeds that could still take one. Weights are the fractional parts, except
		// that a feed which was allotted nothing at all is given a floor — otherwise a feed
		// whose share is a whole number of places can never pick up a remainder, and a feed
		// whose share rounds to exactly zero has weight zero and is silenced by arithmetic
		// rather than by anybody's decision.
		sum := 0.0
		for i, id := range ids {
			if quota[id] < left[id] {
				sum += max(fraction[i], 0.01)
			}
		}
		if sum == 0 {
			break
		}
		roll := rng.Float64() * sum
		picked := ""
		for i, id := range ids {
			if quota[id] >= left[id] {
				continue
			}
			roll -= max(fraction[i], 0.01)
			if roll < 0 {
				picked = id
				break
			}
		}
		if picked == "" {
			break
		}
		quota[picked]++
		spent++
	}
	return quota
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

	// panoramaWeights are for a card whose picture is a band — much wider than it is tall.
	//
	// Weighted towards the widths that give a band somewhere to be. A 10:1 picture in a
	// quarter-page column is sixty-five pixels of photograph over a headline, which reads as
	// a rule that has gone wrong rather than as a picture; across twelve tracks the same file
	// is a hundred and fifty, which is a band across a page and a thing newspapers have done
	// for a century.
	panoramaWeights = []int{2, 3, 1}

	// uprightWeights are for a card whose picture stands taller than it is wide, and they are
	// not weights at all: the narrowest of the three, always.
	//
	// The opposite problem to a panorama, and it is the worse one. Width costs an upright
	// picture height it cannot spend — across the full page a portrait is bounded at 70vh and
	// what survives is a slice through the middle of somebody's photograph. Eight tracks is
	// where the two rules stop fighting: still one of the wide slots, so the card is a
	// landmark and the page keeps the count of them it was laid out with, and narrow enough
	// that the picture is a picture. It is also the width at which a picture can be set beside
	// its story rather than above it, which is the one arrangement an upright picture is
	// better in than a square one. See ArticleCard.
	uprightWeights = []int{0, 0, 1}
)

// panoramaRatio is the shape past which a picture is a band rather than a picture.
//
// Five to two, which is the same number the reader stops drawing its own shapes at — see
// shapeOf in web/src/apps/reader/ArticleCard.tsx. The two are not required to agree and it
// would be strange if they did not: past this the client draws a picture as whatever it is
// instead of squaring it, and this is the width that gives it room to be that.
const panoramaRatio = 5.0 / 2.0

// pictureShape is what a picture's proportions say about how wide to lay its card out.
type pictureShape int

const (
	// pictureOrdinary is everything between the two — and everything nothing has measured,
	// which is the ordinary case for anything published in the last few minutes. An unmeasured
	// picture is laid out exactly as this always laid pictures out; a measurement is what buys
	// the other two.
	pictureOrdinary pictureShape = iota
	pictureWide
	pictureUpright
)

func shapeOfPicture(item *store.Item) pictureShape {
	if item == nil || item.ImageURL == "" || item.ImageWidth <= 0 || item.ImageHeight <= 0 {
		return pictureOrdinary
	}
	switch ratio := float64(item.ImageWidth) / float64(item.ImageHeight); {
	case ratio > panoramaRatio:
		return pictureWide
	case ratio < 1:
		return pictureUpright
	default:
		return pictureOrdinary
	}
}

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
		// What the picture is shaped like decides the width, over both of those. Which cards
		// are widened is a question about the page — where the landmarks fall, how often —
		// and it is answered above without looking at any of them. How wide *this* one goes
		// is a question about this article, and the picture is the part of it with a shape.
		switch shapeOfPicture(picks[pos].Item) {
		case pictureWide:
			weights = panoramaWeights
		case pictureUpright:
			weights = uprightWeights
		}
		picks[pos].Slot = drawSlot(rng, weights)
	}

	// A band is never left in a column, whether or not it was one of the cards this page
	// picked out.
	//
	// Everything above is about the page: how many landmarks it has, where they fall, how wide
	// each goes. This is not about the page. A picture four times wider than it is tall, in a
	// quarter of sixteen tracks, is sixty-five pixels of photograph over a headline — it does
	// not read as a small picture, it reads as a mistake, and no amount of it being the right
	// number of landmarks makes that card work. Half the page is the narrowest width at which
	// the thing is legible, so that is the floor.
	//
	// It does mean a page of bands comes out wider than one card in four. That is the correct
	// answer to a page of bands: the alternative is a column of slivers, chosen so that a rule
	// about landmarks could hold on a page that has none.
	for i := range picks {
		if picks[i].Slot != store.SlotStandard {
			continue
		}
		if shapeOfPicture(picks[i].Item) == pictureWide {
			picks[i].Slot = store.SlotFeature
		}
	}
}
