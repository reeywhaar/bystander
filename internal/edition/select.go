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

// maxFruitlessRounds is how many rounds in a row may place nothing before a band gives up.
//
// A backstop, not an exit condition. The real one is a round in which no feed had anything
// left to be asked for — see [Select] — and this exists because that one terminates with
// probability one rather than in bounded time. Every feed still in the running has a priority
// of at least 1, so it advances with at least a one-in-a-hundred chance per round and the loop
// cannot deadlock; nothing in the arithmetic says when.
//
// Ten thousand, which is not a tuning knob and is not meant to be reached. Because the odds are
// lifted so the live feeds always sum to at least [priorityScale] — see [lift] — a round comes
// up empty with probability at most 1/e, whatever anybody has set. A page that genuinely still
// had something to draw would have to lose that bet ten thousand times running, which is
// e^-10000. Against that, a fruitless round costs one pass over the feed list, so the worst
// this can spend before giving up is a few milliseconds.
const maxFruitlessRounds = 10_000

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
// # Priority is the odds a feed is asked, and it is asked exactly as often as every other
//
// One pass over the feeds is a round: each feed in turn is asked for its next article, and
// hands one over with odds set by its priority. Rounds run until the page is full or nothing
// is left. That is the whole model, and the properties that matter fall straight out of it.
//
// The odds are the feed's priority against [priorityScale], lifted when the feeds still in the
// running sum to less than that — see [lift], which is a change to how long composing takes and
// not to what it produces.
//
// **Volume buys nothing.** A feed is visited once per round whether it has four articles
// waiting or four hundred, so a publisher posting two hundred times a day is asked exactly as
// often as one posting twice. Any scheme that picks *articles* rather than feeds hands the
// page to whoever writes most.
//
// **The slider is linear.** A feed at 100 contributes ten times what a feed at 10 does, and
// five times what one at 20 does, because its odds per round are ten and five times theirs.
// Nothing has to be normalised against anything, which is what makes that true no matter how
// many feeds there are or what else is on the page.
//
// **Tags take no part.** A tag decides whether a feed is on this page at all, which is
// edition.eligible and the page's own filter lists. It does not weigh anything: a tag
// priority meant a feed carrying three tags was drawn from three buckets and took a quarter
// of the page at the same slider setting as a feed carrying one.
//
// # Why not quotas
//
// This used to allot each feed `size × priority / Σ priorities` and hand out the places left
// over — there are always some, since the shares almost never come out whole — by a weighted
// draw on the fractional parts. On paper that has exactly the right expectation. On a real
// subscription list it did not survive contact with the queues.
//
// A page of ninety drawn from fourteen feeds with anything fresh: the whole parts accounted
// for twenty-seven places and *sixty-three* went to the leftover draw, because ten of those
// feeds had one to three articles each and everything their share could not use fell through.
// The leftover draw weighted a feed by the fractional part of its share, which is a sawtooth
// of priority rather than priority — `frac(90×10/500)` is 0.8 and `frac(90×25/500)` is 0.5,
// so the feed at 10 drew at 23.5% a place against 14.7% for the feed at 25, and the feed at 5
// drew best of all at 26.5%. Every feed at 50 computed 90×50/500 = 9.00 exactly, a fractional
// part of zero, and was floored to the 0.01 minimum that existed to stop feeds being silenced
// by arithmetic. It silenced the whole priority-50 cohort instead.
//
// What that looked like: the Guardian, at priority 10, took 24 of 90 places on a live front
// page — a mean of 26.7 over 400 seeds, against 20.4 for Hacker News at 25. Under the round
// robin the same queues give 13.5 and 34.3, a ratio of 2.54 where the sliders say 2.5.
//
// There is no arithmetic here to get wrong. A feed is asked, or it is not.
//
// # When a feed runs out
//
// Nothing is redistributed. Its neighbours keep being asked at their own rate and the page
// takes more rounds to fill, which is the same page arrived at more slowly. Under quotas the
// places a thin feed could not use were handed back and re-apportioned over whoever still had
// something — and whoever still has something is always the firehose, so priority stopped
// governing the moment the quiet feeds ran dry, which on a real list is within two rounds.
//
// A page still comes up short when every queue is dry, and that is the honest answer rather
// than something to pad.
//
// # How a band ends
//
// Two conditions, and a backstop. A band stops when the page is full, or when a round finds
// that no feed has anything left *that could go on the page* — which is not the same as a
// round that placed nothing. A round can legitimately come up empty about a third of the time,
// and a page composed of quiet feeds would be empty if that ended it.
//
// Every feed still in the running has odds above zero and cursors only ever move forward, so
// there is no state the loop can sit in with nothing left to happen. That makes it terminate
// with probability one, which is not the same as terminating — ten feeds at 10% leave a round
// empty about a third of the time, and nothing stops empty rounds recurring — so
// [maxFruitlessRounds] bounds a run of them.
//
// [lift] does not change that. It shortens how long the loop is expected to run and leaves the
// question of whether it stops exactly where it was.
//
// # A band at a time, across every feed
//
// Three passes over the same queues, one per band, and the round robin runs afresh in each.
// Everything new from every feed is placed before anything already seen from any feed; every
// unread repeat before any read one. A pass per band rather than a queue read straight
// through, because otherwise a feed that rolls well and has nothing new contributes something
// already read while another feed still has unread articles waiting.
//
// A page with room left over and nothing new to put in it looks broken rather than honest,
// which is what the later passes are for. Nothing is invented: a repeat that was read arrives
// with its read mark, so it is greyed rather than pretending to be new.
func Select(sources map[string]*Source, size int, seed int64) []store.Pick {
	if size <= 0 {
		return nil
	}

	// Zero means never — a real setting, and how somebody keeps a feed subscribed but out of
	// rotation. Dropped here rather than left in at odds of zero, so the rounds below do not
	// spend their lives asking a feed that has already answered.
	feeds := make([]string, 0, len(sources))
	for id, src := range sources {
		if src != nil && src.Priority > 0 {
			feeds = append(feeds, id)
		}
	}
	// Sorted before anything is drawn, because a map's order is not stable and a page that
	// cannot be replayed from its seed is the one thing the seed is for.
	slices.Sort(feeds)

	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>32|1))

	var picks []store.Pick
	taken := make(map[string]bool, size)
	cursor := make(map[string]int, len(feeds))
	// dropped is the feeds found to have nothing left, so a band stops walking them.
	dropped := make(map[string]bool, len(feeds))

	for _, band := range []func(*Source) []*store.Item{
		func(s *Source) []*store.Item { return s.Fresh },
		func(s *Source) []*store.Item { return s.Unread },
		func(s *Source) []*store.Item { return s.Read },
	} {
		clear(cursor)
		clear(dropped)
		fruitless := 0

		// total is what the odds are out of: the priorities of every feed that still has
		// something to offer. Kept as the round goes rather than recounted before each one,
		// because recounting means a second pass over every feed for every round of every
		// page — measured at twice the cost of composing on a real subscription list, to
		// learn something the previous round already knew.
		//
		// A feed that empties is subtracted the moment it is found empty, which is one round
		// after it actually emptied. The odds are that round's share too low, by the share of
		// a feed with nothing left to give. It costs nothing and corrects itself.
		total := 0
		for _, id := range feeds {
			total += sources[id].Priority
		}

		for len(picks) < size {
			if total <= 0 {
				break
			}
			// Redrawn every round rather than once, so no feed is permanently the one that
			// gets first refusal on a link two feeds both carry. Ploum.net's only fresh
			// article was the same URL Hacker News and Lobsters had both picked up; under a
			// fixed order it lost that race on every seed and the feed never appeared at
			// all.
			rng.Shuffle(len(feeds), func(i, j int) { feeds[i], feeds[j] = feeds[j], feeds[i] })

			// live is what ends the loop, and it is deliberately not "did this round place
			// anything". A round can legitimately place nothing — that is what odds are —
			// and stopping there would end the page early for no reason. What ends it is a
			// round in which no feed had anything left to be asked for.
			live := false
			placed := 0
			odds := lift(total)
			for _, id := range feeds {
				if dropped[id] {
					continue
				}
				items := band(sources[id])

				// Step over anything already on the page before rolling, so a roll is
				// always spent on an article that could actually be placed.
				//
				// This does not change the page. Rolling first and stepping after gives
				// identical counts — measured over 400 seeds at four priorities against a
				// feed mirroring thirty of another's articles, to the last article — because
				// a successful roll steps over the whole run at once either way. What it
				// saves is a feed whose remaining queue is nothing but pieces another feed
				// already carried waiting on a successful roll to discover there is nothing
				// behind them.
				//
				// What makes two rows the same article is the link, not the id. A
				// publication carried in two feeds is two rows — the same piece at
				// dataengineeringweekly.com/feed and at its Substack mirror has two ids,
				// because an item belongs to the feed it arrived in and feeds are shared
				// between everybody following them. Deduping on the id let both onto the
				// page, one above the other, and on live data 46 links were held by more
				// than one feed.
				//
				// The link exactly, never the title. The one pair of same-titled articles on
				// that instance was three different publications' "Coming soon" placeholder,
				// and merging those would lose two real articles to save nobody from a
				// duplicate.
				for cursor[id] < len(items) && taken[identify(items[cursor[id]])] {
					cursor[id]++
				}
				if cursor[id] >= len(items) {
					dropped[id] = true
					total -= sources[id].Priority
					continue
				}
				live = true

				if rng.Float64()*priorityScale >= float64(sources[id].Priority)*odds {
					continue
				}
				item := items[cursor[id]]
				cursor[id]++
				taken[identify(item)] = true
				picks = append(picks, store.Pick{Item: item})
				placed++
				if len(picks) >= size {
					break
				}
			}
			if !live {
				break
			}

			if placed > 0 {
				fruitless = 0
				continue
			}
			if fruitless++; fruitless >= maxFruitlessRounds {
				break
			}
		}
	}

	for i := range picks {
		picks[i].Rank = i
	}
	assignSlots(picks, rng)
	return picks
}

// identify is what makes two rows the same article for the purposes of one page.
//
// The link, which is the publisher's own name for the piece and is the same wherever it is
// syndicated. Falling back to the id where a feed gave no link, so that link-less articles are
// each themselves rather than all one.
func identify(item *store.Item) string {
	if item.Link != "" {
		return item.Link
	}
	return "id:" + item.ID
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

// priorityScale is what a priority is out of. A feed at 100 hands something over on every
// round it is asked, one at 25 on a quarter of them.
//
// It is also the floor the live priorities are lifted to, which is [lift].
const priorityScale = 100

// lift is how much the odds of every live feed are scaled up this round.
//
// One when the live priorities already sum to [priorityScale] or more, and otherwise however
// much it takes to get them there. Ten feeds at 1 are asked as ten feeds at 10.
//
// # It does not change the page
//
// Every live feed is scaled by the same constant, so every ratio between them is untouched,
// and the ratios are the whole of what the sliders mean. Measured against the unlifted sampler
// over 400 seeds on a real front page and seven synthetic ones, the two agree on every feed to
// within the noise of the draw.
//
// # What it changes is how long
//
// Lifted, the live priorities sum to at least priorityScale, so a round places one article in
// expectation and a page of n articles takes about n rounds. Unlifted, a page whose feeds are
// all set to 1 needs about a hundred rounds per article: the same page, arrived at a hundred
// times more slowly. Composing one from a single feed at priority 1 went from 418µs to 9µs,
// ten feeds at 1 from 631µs to 72µs, and an ordinary subscription list is a shade quicker too.
//
// It is **not** a termination guarantee, and it is worth saying so because the arithmetic looks
// like one. A round still comes up empty often — ten feeds at 10% leave one empty 0.9^10 of the
// time, about a third — and nothing here stops empty rounds recurring. What the lift bounds is
// the *rate*: the chance of an empty round is the product of (1 - each feed's odds), which under
// a sum of at least one is largest when the odds are spread thinnest — n feeds at 1/n, giving
// (1-1/n)^n, which climbs towards 1/e and never reaches it. Under 37%, whatever anybody sets.
// Whether the loop stops is still [maxFruitlessRounds]' job, exactly as it was before.
func lift(total int) float64 {
	if total >= priorityScale {
		return 1
	}
	return float64(priorityScale) / float64(total)
}
