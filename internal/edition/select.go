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
// # A fill round: one line, one number, one article
//
// Every feed still holding something is laid end to end along a line, each taking a length
// equal to its priority. One number lands somewhere on that line and names the feed it fell
// in; that feed hands over the article at the front of its queue. Then the line is rebuilt and
// another number is thrown, until the page is full or the line is empty.
//
// Three feeds at 10, 20 and 70 make a line where the first tenth belongs to the first, the
// next fifth to the second, and the remaining seven tenths to the third — so 0.2 picks the
// second and 0.5 picks the third. That is the whole model, and everything below falls out of
// it.
//
// **Volume buys nothing.** A feed's length on the line is its priority, not its backlog, so a
// publisher posting two hundred times a day is exactly as likely to be picked as one posting
// twice. Any scheme that picks *articles* rather than feeds hands the page to whoever writes
// most.
//
// **The slider is linear.** A feed at 100 takes ten times the line a feed at 10 does and is
// picked ten times as often. The line is rebuilt from whoever is left, so the shares are
// always over the feeds that can actually contribute — nothing has to be normalised by hand.
//
// **Tags take no part.** A tag decides whether a feed is on this page at all, which is
// edition.eligible and the page's own filter lists. It does not weigh anything: a tag
// priority meant a feed carrying three tags was drawn from three buckets and took a quarter
// of the page at the same slider setting as a feed carrying one.
//
// # Why the work is bounded
//
// Every turn around the loop does exactly one of two things: it places an article, or it finds
// the feed it picked has nothing left and takes that feed off the line. Nothing else can
// happen. So a band finishes in at most size+len(feeds) turns — a count, not a probability,
// and the reason there is no backstop in here.
//
// That is what this is for. It replaced a round robin that asked every feed in turn at its own
// odds: the same page, to within the noise of the draw, but a round could legitimately place
// nothing, so the work per page was unbounded and had to be capped at ten thousand fruitless
// rounds on the strength of an argument about 1/e. Measured against a real subscription list
// the two agree on every feed's mean to within 0.3 of an article, and this one composes a
// ninety-article front page in 70µs against 124µs.
//
// What it costs is a little consistency. A round robin asks each feed at most once per round,
// which is stratified sampling and holds a feed's share closer to its slider on any single
// page; independent draws let a feed come up twice in a row. On that same front page Hacker
// News went from 34.1±2.6 to 34.0±3.2 — the same page, wobbling about a fifth more.
//
// # Why the line is shuffled
//
// It does not change the distribution. A uniform number cares how *long* a stretch of the line
// is, not where it sits, and over four thousand pages the shuffled and unshuffled lines agree
// on every feed's mean and spread to within noise. It is here so that nothing about a page can
// depend on the order feed ids happen to sort in — a property worth having even while nothing
// reads it, and it costs about 20µs a page.
//
// # When a feed runs out
//
// It comes off the line, and the rest carry on in the same proportions to each other. Nothing
// is redistributed by rule: under the quota sampler this replaced, the places a thin feed could
// not fill were handed back and re-apportioned over whoever still had something — and whoever
// still has something is the firehose, so priority stopped governing the moment the quiet feeds
// ran dry, which on a real list is within two rounds. The Guardian at priority 10 was taking 24
// of 90 places against 23 for Hacker News at 25.
//
// A page still comes up short when every queue is dry, and that is the honest answer rather
// than something to pad.
//
// # A band at a time, across every feed
//
// Three passes over the same queues, one per band, and the line is built afresh in each.
// Everything new from every feed is placed before anything already seen from any feed; every
// unread repeat before any read one. A pass per band rather than a queue read straight through,
// because otherwise a feed that is picked early and has nothing new contributes something
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
	// rotation. Dropped here rather than given a zero-length stretch of the line, so it cannot
	// be landed on by a number that falls exactly on its edge.
	feeds := make([]string, 0, len(sources))
	for id, src := range sources {
		if src != nil && src.Priority > 0 {
			feeds = append(feeds, id)
		}
	}
	// Sorted before anything is drawn, because a map's order is not stable and a page that
	// cannot be replayed from its seed is the one thing the seed is for. The shuffle below
	// needs a settled order to shuffle.
	slices.Sort(feeds)

	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>32|1))

	var picks []store.Pick
	taken := make(map[string]bool, size)
	cursor := make(map[string]int, len(feeds))
	// live is who is still on the line, reused across bands.
	live := make([]string, 0, len(feeds))

	for _, band := range []func(*Source) []*store.Item{
		func(s *Source) []*store.Item { return s.Fresh },
		func(s *Source) []*store.Item { return s.Unread },
		func(s *Source) []*store.Item { return s.Read },
	} {
		clear(cursor)
		live = append(live[:0], feeds...)

		for len(picks) < size && len(live) > 0 {
			rng.Shuffle(len(live), func(i, j int) { live[i], live[j] = live[j], live[i] })

			total := 0
			for _, id := range live {
				total += sources[id].Priority
			}
			// Walked rather than binary-searched. The line is rebuilt every turn anyway —
			// that is what the shuffle costs — so a cumulative slice to search would be built
			// in the same pass that this one throws the number in.
			roll := rng.Float64() * float64(total)
			at, run := len(live)-1, 0.0
			for i, id := range live {
				run += float64(sources[id].Priority)
				if roll < run {
					at = i
					break
				}
			}
			id := live[at]

			items := band(sources[id])
			// Step over anything already on the page.
			//
			// What makes two rows the same article is the link, not the id. A publication
			// carried in two feeds is two rows — the same piece at dataengineeringweekly.com
			// and at its Substack mirror has two ids, because an item belongs to the feed it
			// arrived in and feeds are shared between everybody following them. Deduping on the
			// id let both onto the page, one above the other, and on live data 46 links were
			// held by more than one feed.
			//
			// The link exactly, never the title. The one pair of same-titled articles on that
			// instance was three different publications' "Coming soon" placeholder, and merging
			// those would lose two real articles to save nobody from a duplicate.
			for cursor[id] < len(items) && taken[identify(items[cursor[id]])] {
				cursor[id]++
			}
			if cursor[id] >= len(items) {
				// Off the line. This is the other half of the bound: a turn that places
				// nothing has shortened the line instead, so there can only be len(feeds) of
				// them.
				live = append(live[:at], live[at+1:]...)
				continue
			}

			item := items[cursor[id]]
			cursor[id]++
			taken[identify(item)] = true
			picks = append(picks, store.Pick{Item: item})
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

// slotWidth is how wide a card in each slot is drawn, in CSS pixels — the widest it ever gets,
// which is not always on the widest screen.
//
// Measured in a browser against the real stylesheet rather than worked out from the track
// arithmetic, and the two disagree. The responsive rules shorten the widths onto the same
// sixteen tracks instead of changing the track count, so at an 820-pixel viewport a feature
// card spans all sixteen and comes out at 772 — wider than the 662 it gets on a full-size page.
// Deriving these from four-of-sixteen would have taken the bound from the narrower number and
// let the tablet blur through.
var slotWidth = map[store.Slot]int{
	store.SlotLead:     1352,
	store.SlotWide:     1007,
	store.SlotFeature:  772,
	store.SlotStandard: 512,
	store.SlotBrief:    512,
}

// maxUpscale is how far past its own size a picture may be drawn.
//
// Twice. A photograph at double is soft if somebody looks for it and unremarkable if they do
// not; the failure this is against is not that subtle. A feed publishing 140-pixel thumbnails —
// a real one, and it publishes nothing else — was having them drawn 1352 pixels wide across the
// top of a page, nine and a half times their own size, which is not a photograph any more.
//
// Chosen by sweeping it against a real subscription list rather than by taste. Tighter costs
// landmarks and buys nothing: at 1.5 the page keeps 20.3 wide cards out of the 21.8 it would
// lay out with no bound at all, at 2.0 it keeps 21.3, and neither draws anything past twice.
// Looser starts letting real stretch back in — at 2.5, wide cards begin carrying pictures at
// two and a half times, which is where the softness stops being something you have to look for.
//
// At 2.0 every picture on that list is drawn inside the bound except the ones held there by the
// floor below, which is as well as this can be asked to do.
const maxUpscale = 2.0

// widestSlotFor is the widest a card may be laid out without drawing its picture past
// [maxUpscale].
//
// [store.SlotStandard] is a floor rather than an answer of last resort. A card narrower than a
// column is not something this layout has, so a picture too small even for that is still drawn
// to fill its card — what this decides is only how far up from there the card may go.
//
// A picture nobody has measured caps nothing, which is the ordinary case for anything published
// in the last few minutes — see internal/jobs. The guess would be a guess in both directions,
// and flattening every page composed in the minutes before the measurer catches up is the worse
// of the two mistakes.
func widestSlotFor(item *store.Item) store.Slot {
	if item == nil || item.ImageURL == "" || item.ImageWidth <= 0 {
		return store.SlotLead
	}
	room := float64(item.ImageWidth) * maxUpscale
	// wideSlots is widest first, so the first that fits is the widest that does.
	for _, slot := range wideSlots {
		if float64(slotWidth[slot]) <= room {
			return slot
		}
	}
	return store.SlotStandard
}

// narrower is whichever of two slots is drawn narrower.
func narrower(a, b store.Slot) store.Slot {
	if slotWidth[a] <= slotWidth[b] {
		return a
	}
	return b
}

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
		// Held to what the picture can carry. The draw decides how wide this card would
		// like to be; the picture decides how wide it may be, and a picture drawn past
		// [maxUpscale] costs the page more than a landmark buys it.
		picks[pos].Slot = narrower(drawSlot(rng, weights), widestSlotFor(picks[pos].Item))
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
			// And still held to what the picture can carry. A band too small to be widened
			// stays in its column: a sharp short band reads as a band, where the same file
			// stretched to half the page reads as a mistake at both jobs at once.
			picks[i].Slot = narrower(store.SlotFeature, widestSlotFor(picks[i].Item))
		}
	}
}
