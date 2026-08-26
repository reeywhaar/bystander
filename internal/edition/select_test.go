package edition

import (
	"fmt"
	"testing"

	"bystander/internal/store"
)

// feed builds a source with n articles, all of them layout-worthy so slot assignment is
// driven by rank rather than by missing images.
func feed(id string, priority, n int) *Source {
	items := make([]*store.Item, n)
	for i := range n {
		items[i] = &store.Item{
			ID:       fmt.Sprintf("%s-%d", id, i),
			FeedID:   id,
			GUID:     fmt.Sprintf("%s-%d", id, i),
			Title:    fmt.Sprintf("%s article %d", id, i),
			Summary:  "a summary",
			ImageURL: "https://example.com/i.png",
		}
	}
	return &Source{FeedID: id, Priority: priority, Fresh: items}
}

func sources(list ...*Source) map[string]*Source {
	out := make(map[string]*Source, len(list))
	for _, s := range list {
		out[s.FeedID] = s
	}
	return out
}

// Same seed, same page. This is what makes a generation replayable when something looks
// wrong, and it is the reason the sampler takes a seed rather than reaching for a clock.
func TestSelectIsDeterministic(t *testing.T) {
	src := sources(feed("a", 50, 40), feed("b", 50, 40), feed("c", 50, 40))

	first := Select(src, 20, 12345)
	second := Select(src, 20, 12345)

	if len(first) != len(second) {
		t.Fatalf("two runs of one seed gave %d and %d articles", len(first), len(second))
	}
	for i := range first {
		if first[i].Item.ID != second[i].Item.ID {
			t.Fatalf("rank %d: %q then %q", i, first[i].Item.ID, second[i].Item.ID)
		}
	}
}

// Priority is a probability, not an ordering: a feed at 90 appears more often than one at
// 10 without silencing it.
func TestPriorityShiftsTheOdds(t *testing.T) {
	// More feeds than the page has room for, so the cap does not fill it by itself and
	// the odds are what decide. With two feeds and a page they could both fill, every
	// priority produces the same page — correct, and useless as a test.

	counts := map[string]int{}
	for seed := range 300 {
		list := []*Source{feed("loud", 90, 100)}
		for i := range 9 {
			list = append(list, feed(fmt.Sprintf("quiet%d", i), 10, 100))
		}
		for _, pick := range Select(sources(list...), 10, int64(seed)) {
			counts[pick.Item.FeedID]++
		}
	}

	for i := range 9 {
		quiet := fmt.Sprintf("quiet%d", i)
		if counts["loud"] <= counts[quiet] {
			t.Errorf("the feed at 90 contributed %d against %s at 10 with %d", counts["loud"], quiet, counts[quiet])
		}
		// Silencing is the failure the probabilistic model exists to avoid.
		if counts[quiet] == 0 {
			t.Errorf("%s never appeared at all", quiet)
		}
	}
}

// Zero means never, and it must not merely be improbable.
func TestZeroPriorityNeverAppears(t *testing.T) {
	for seed := range 50 {
		src := sources(feed("on", 50, 50), feed("off", 0, 50))
		for _, pick := range Select(src, 20, int64(seed)) {
			if pick.Item.FeedID == "off" {
				t.Fatalf("seed %d drew from a feed at priority zero", seed)
			}
		}
	}
}

func TestAllZeroTerminates(t *testing.T) {
	src := sources(feed("a", 0, 10), feed("b", 0, 10))
	if got := Select(src, 20, 1); len(got) != 0 {
		t.Fatalf("Select() returned %d articles from an all-zero pool", len(got))
	}
}

// Volume buys nothing. A draw picks a feed and then takes one article from it, so a feed
// with five hundred articles is drawn exactly as often as one with sixty at the same
// priority — which is why no per-feed cap is needed to stop a prolific publisher taking
// the page.
func TestVolumeDoesNotBuyAShareOfThePage(t *testing.T) {
	const size = 40
	counts := map[string]int{}
	for seed := range 100 {
		src := sources(feed("prolific", 50, 500), feed("occasional", 50, 200))
		for _, pick := range Select(src, size, int64(seed)) {
			counts[pick.Item.FeedID]++
		}
	}

	// Equal priorities, wildly unequal backlogs: the split should be close to even.
	prolific, occasional := counts["prolific"], counts["occasional"]
	ratio := float64(prolific) / float64(occasional)
	if ratio < 0.85 || ratio > 1.18 {
		t.Errorf("a feed with 500 articles took %d of the page against %d for one with 200 (ratio %.2f)",
			prolific, occasional, ratio)
	}
}

// The page fills from whoever is left when a small feed runs out, rather than coming up
// short. Nobody wants two thirds of a page and no explanation.
func TestASmallFeedDoesNotStarveThePage(t *testing.T) {
	const size = 40
	src := sources(feed("plenty", 50, 500), feed("trickle", 50, 5))

	picks := Select(src, size, 7)
	if len(picks) != size {
		t.Fatalf("Select() returned %d articles, want a full page of %d", len(picks), size)
	}

	counts := map[string]int{}
	for _, pick := range picks {
		counts[pick.Item.FeedID]++
	}
	if counts["trickle"] != 5 {
		t.Errorf("the small feed contributed %d of its 5 articles", counts["trickle"])
	}
}

// The failure that removing the cap was for: with a handful of feeds, the weights have to
// decide the mix. A cap set at a fraction of the page could not leave them room.
func TestPriorityDecidesWithFewFeeds(t *testing.T) {
	const size = 20
	counts := map[string]int{}
	for seed := range 200 {
		src := sources(feed("loud", 90, 200), feed("quiet", 10, 200))
		for _, pick := range Select(src, size, int64(seed)) {
			counts[pick.Item.FeedID]++
		}
	}

	// 90 against 10 is nine times the weight; anything close to even means the mix is
	// being decided by something other than the sliders.
	if ratio := float64(counts["loud"]) / float64(counts["quiet"]); ratio < 4 {
		t.Errorf("a feed at 90 took %d of the page against %d for one at 10 (ratio %.2f); the sliders are not deciding",
			counts["loud"], counts["quiet"], ratio)
	}
	if counts["quiet"] == 0 {
		t.Error("the quieter feed never appeared at all")
	}
}

// One article, one place. A feed's bands are one queue, and an article that was placed in an
// earlier pass must not be offered again in a later one.
func TestNoArticleIsPlacedTwice(t *testing.T) {
	src := feed("a", 50, 12)
	// The same three articles listed as fresh and again as read, which is what a queue built
	// from an inconsistent read set would look like.
	src.Read = src.Fresh[:3]

	seen := map[string]bool{}
	for _, pick := range Select(sources(src), 20, 3) {
		if seen[pick.Item.ID] {
			t.Fatalf("article %q appears twice", pick.Item.ID)
		}
		seen[pick.Item.ID] = true
	}
}

// A short edition is the honest outcome of a dry pool, not something to pad.
func TestExhaustedPoolGivesAShortPage(t *testing.T) {
	src := sources(feed("a", 50, 3), feed("b", 50, 2))

	got := Select(src, 60, 9)
	if len(got) != 5 {
		t.Fatalf("Select() returned %d articles, want the 5 that exist", len(got))
	}
	for i, pick := range got {
		if pick.Rank != i {
			t.Errorf("rank %d is stamped %d", i, pick.Rank)
		}
	}
}

// The page opens with weight, and prominence is spread rather than spent at the top.
//
// This replaced a rule that gave rank 0 the lead and ranks 1..4 the features, which ran the
// page big to small and then left forty cards identical. Rank is draw order out of a weighted
// sample, not an editor's judgement, so there was never anything to preserve by stacking
// prominence at the front — and a page of identical cards is a page with one landmark on it.
func TestThePageOpensWideAndSpreadsTheRest(t *testing.T) {
	var list []*Source
	var ids []string
	for i := range 6 {
		id := fmt.Sprintf("f%d", i)
		list = append(list, feed(id, 50, 200))
		ids = append(ids, id)
	}

	// Several seeds, because these are drawn: one page passing says little.
	for seed := int64(1); seed <= 12; seed++ {
		picks := Select(sources(list...), 50, seed)
		if len(picks) < 20 {
			t.Fatalf("seed %d: only %d articles; this test needs a full page", seed, len(picks))
		}

		// Never a single column at the top. A front page that began with four narrow ones
		// would have nothing to look at first.
		switch picks[0].Slot {
		case store.SlotLead, store.SlotWide, store.SlotFeature:
		default:
			t.Errorf("seed %d: the page opens with %q", seed, picks[0].Slot)
		}

		wide := []int{}
		for i, p := range picks {
			switch p.Slot {
			case store.SlotLead, store.SlotWide, store.SlotFeature:
				wide = append(wide, i)
			}
		}
		if len(wide) < 2 {
			t.Errorf("seed %d: %d wide cards on a page of %d", seed, len(wide), len(picks))
		}

		// Spread, not stacked. The old rule put them all in the first five ranks; the test
		// that matters is that at least one is well down the page.
		if last := wide[len(wide)-1]; last < len(picks)/2 {
			t.Errorf("seed %d: the last wide card is at rank %d of %d — prominence is still "+
				"bunched at the top", seed, last, len(picks))
		}

		// Two wide cards next to each other in reading order is fine and is not checked
		// for. Adjacent here does not mean side by side: `dense` places them, and a ten
		// beside a six is a row that tiles exactly. Asserting otherwise would be asserting
		// a guess about the grid.
	}
}

// Nothing is ever narrower than a quarter of the grid.
//
// The grid is sixteen tracks so that widths need not be multiples of a column, which is what
// lets a row fail to tile — but a card of one or two tracks would be a hundred pixels of
// squeezed text, and there is no story worth reading in that. The gaps a row leaves when it
// does not add up stay white; they never become a card.
func TestNoArticleIsNarrowerThanAQuarterOfThePage(t *testing.T) {
	tracks := map[store.Slot]int{
		store.SlotLead:     16,
		store.SlotWide:     12,
		store.SlotFeature:  8,
		store.SlotStandard: 4,
		store.SlotBrief:    4,
	}
	narrowest := 4

	var list []*Source
	var ids []string
	for i := range 6 {
		id := fmt.Sprintf("f%d", i)
		list = append(list, feed(id, 50, 200))
		ids = append(ids, id)
	}

	for seed := int64(1); seed <= 12; seed++ {
		for _, p := range Select(sources(list...), 50, seed) {
			span, known := tracks[p.Slot]
			if !known {
				t.Fatalf("seed %d: %q has no width; styles.css will not know either",
					seed, p.Slot)
			}
			if span*5 < 16 {
				t.Errorf("seed %d: %q is %d/16, under a fifth of the page", seed, p.Slot, span)
			}
			// And a multiple of the narrowest, which is what guarantees a row can never
			// strand a gap nothing fits: the remainder is then a multiple of it too.
			if span%narrowest != 0 {
				t.Errorf("seed %d: %q is %d/16, not a multiple of the narrowest %d/16 — a row "+
					"holding one could leave a gap no card fits", seed, p.Slot, span, narrowest)
			}
		}
	}
}

// A card sized for a picture that has no picture is what makes a page look broken.
func TestArticlesWithNothingToShowBecomeBriefs(t *testing.T) {
	bare := &Source{FeedID: "bare", Priority: 50, Fresh: []*store.Item{
		{ID: "bare-0", FeedID: "bare", GUID: "bare-0", Title: "No picture, no words"},
	}}

	picks := Select(sources(bare), 10, 1)
	if len(picks) != 1 {
		t.Fatalf("Select() returned %d articles, want 1", len(picks))
	}
	// Rank 0 would ordinarily be the lead.
	if picks[0].Slot != store.SlotBrief {
		t.Errorf("slot = %q, want brief", picks[0].Slot)
	}
}

// shaped is a feed whose every picture is the same measured shape.
func shaped(id string, priority, n, w, h int) *Source {
	src := feed(id, priority, n)
	for _, item := range src.Fresh {
		item.ImageWidth, item.ImageHeight = w, h
	}
	return src
}

// slotsFrom lays out several pages and tallies the widths every card came out at.
func slotsFrom(t *testing.T, src *Source, seeds int) map[store.Slot]int {
	t.Helper()
	tally := map[store.Slot]int{}
	for seed := int64(1); seed <= int64(seeds); seed++ {
		picks := Select(sources(src), 50, seed)
		if len(picks) < 20 {
			t.Fatalf("seed %d: only %d articles; this test needs a full page", seed, len(picks))
		}
		for _, p := range picks {
			tally[p.Slot]++
		}
	}
	return tally
}

// A picture much wider than it is tall is never left in a quarter-page column.
//
// Not a preference about landmarks — a floor. Four times wider than tall, in four of sixteen
// tracks, is sixty-five pixels of photograph over a headline: not a small picture, a mistake.
// This is the case the first version of the rule missed, because it only chose *which* width
// an already-widened card took, and a band in a card the page had not picked out stayed a
// sliver.
func TestABandIsNeverLeftInAColumn(t *testing.T) {
	bands := slotsFrom(t, shaped("band", 50, 200, 2000, 200), 12)
	if bands[store.SlotStandard] != 0 {
		t.Errorf("%d band pictures were left at a quarter of the page", bands[store.SlotStandard])
	}
	if bands[store.SlotFeature] == 0 {
		t.Error("no band was laid out at half the page, which is the floor this is about")
	}

	// A page of bands comes out wider than one card in four, and that is the answer rather
	// than a side effect: the alternative is a column of slivers chosen so a rule about
	// landmarks could hold on a page that has none.
	wide := bands[store.SlotLead] + bands[store.SlotWide] + bands[store.SlotFeature]
	if total := wide + bands[store.SlotStandard] + bands[store.SlotBrief]; wide*4 <= total {
		t.Errorf("only %d of %d cards were widened on a page of nothing but bands", wide, total)
	}
}

// How wide a widened card goes still follows its picture, on top of the floor.
func TestHowWideACardGoesFollowsItsPicture(t *testing.T) {
	// A band still reaches the widths above the floor — the floor is where it starts, not
	// where it is held. Across the full page a 10:1 file is a band across a newspaper.
	bands := slotsFrom(t, shaped("band", 50, 200, 2000, 200), 12)
	if bands[store.SlotLead] == 0 || bands[store.SlotWide] == 0 {
		t.Errorf("bands never went past the floor: lead %d, wide %d, feature %d",
			bands[store.SlotLead], bands[store.SlotWide], bands[store.SlotFeature])
	}

	// A portrait goes the other way: always the narrowest of the wide slots, because width
	// costs an upright picture height it cannot spend — across the full page it is bounded at
	// 70vh and what survives is a slice through the middle of it. And no floor: a portrait at
	// a quarter of the page is a picture, which is the whole difference from a band.
	uprights := slotsFrom(t, shaped("tall", 50, 200, 800, 1200), 12)
	if uprights[store.SlotLead] != 0 || uprights[store.SlotWide] != 0 {
		t.Errorf("an upright picture was laid out across the page: lead %d, wide %d",
			uprights[store.SlotLead], uprights[store.SlotWide])
	}
	if uprights[store.SlotFeature] == 0 {
		t.Error("upright pictures stopped being widened at all; they should still be landmarks")
	}
	if uprights[store.SlotStandard] == 0 {
		t.Error("upright pictures were all widened; the floor is for bands, not for everything")
	}

	// And a page that has measured nothing is laid out exactly as it always was — every
	// picture on it is pictureOrdinary, which is the case this rule must not disturb.
	plain := slotsFrom(t, feed("plain", 50, 200), 12)
	if plain[store.SlotLead] == 0 || plain[store.SlotWide] == 0 || plain[store.SlotFeature] == 0 {
		t.Errorf("unmeasured pictures stopped drawing the full range: lead %d, wide %d, feature %d",
			plain[store.SlotLead], plain[store.SlotWide], plain[store.SlotFeature])
	}
	widened := plain[store.SlotLead] + plain[store.SlotWide] + plain[store.SlotFeature]
	if total := widened + plain[store.SlotStandard] + plain[store.SlotBrief]; widened*3 > total {
		t.Errorf("an unmeasured page widened %d of %d cards; it should be about one in four",
			widened, total)
	}
}

// A feed with nothing fresh must still be able to offer its repeats.
//
// The bug this covers: the fresh articles and the repeats were two separately built pools,
// each with its own tag buckets, and a bucket lists only the feeds that had something in the
// pool it was planned from. So a page whose feeds had all been read through reached only the
// one feed that still had an unread article, and came back a quarter full with sixty repeats
// sitting there unreachable — worst on exactly the pages that needed them most. Bands in one
// queue cannot come apart that way.
func TestEveryBandReachesEveryFeed(t *testing.T) {
	// One feed with a single fresh article; five feeds' worth of repeats behind it.
	only := feed("a", 50, 5)
	only.Fresh, only.Read = only.Fresh[:1], only.Fresh[1:]

	list := []*Source{only}
	for id, n := range map[string]int{"b": 9, "c": 8, "d": 20, "e": 20} {
		src := feed(id, 50, n)
		src.Fresh, src.Read = nil, src.Fresh
		list = append(list, src)
	}

	picks := Select(sources(list...), 30, 7)
	if len(picks) != 30 {
		t.Fatalf("the page holds %d articles, want a full 30 out of the 62 available", len(picks))
	}
	// The one fresh article is the first thing placed, so it leads the page.
	if picks[0].Item.FeedID != "a" {
		t.Errorf("the page opens with %q; the only fresh article should come first",
			picks[0].Item.FeedID)
	}

	drawn := map[string]bool{}
	for _, pick := range picks {
		drawn[pick.Item.FeedID] = true
	}
	if len(drawn) != 5 {
		t.Errorf("the page draws from %d feeds, want all five: %v", len(drawn), drawn)
	}
}

// A publication carried in two feeds is two rows, and only one of them belongs on the page.
//
// This is what a mirror looks like: the same piece at a publication's own domain and at its
// Substack, two feeds, two item ids, one link. Deduping on the id let both through and the
// page carried the same article twice, one above the other — on live data 46 links were held
// by more than one feed.
func TestOneArticleInTwoFeedsIsShownOnce(t *testing.T) {
	own := &Source{FeedID: "own", Priority: 50}
	mirror := &Source{FeedID: "mirror", Priority: 50}

	// Three shared pieces, plus one only the mirror carries, so the page is not simply
	// short of anything else to draw.
	for i := range 3 {
		link := fmt.Sprintf("https://example.com/p/%d", i)
		own.Fresh = append(own.Fresh, &store.Item{
			ID: fmt.Sprintf("own-%d", i), FeedID: "own", GUID: fmt.Sprintf("own-%d", i),
			Title: fmt.Sprintf("Piece %d", i), Link: link,
		})
		mirror.Fresh = append(mirror.Fresh, &store.Item{
			// A different id, and — as happens on real feeds — sometimes a different
			// title for the same URL.
			ID: fmt.Sprintf("mirror-%d", i), FeedID: "mirror", GUID: fmt.Sprintf("mirror-%d", i),
			Title: fmt.Sprintf("Piece %d (mirrored)", i), Link: link,
		})
	}
	mirror.Fresh = append(mirror.Fresh, &store.Item{
		ID: "mirror-only", FeedID: "mirror", GUID: "mirror-only",
		Title: "Only here", Link: "https://example.com/p/only",
	})

	picks := Select(sources(own, mirror), 20, 99)

	seen := map[string]string{}
	for _, pick := range picks {
		if first, dup := seen[pick.Item.Link]; dup {
			t.Fatalf("%s is on the page twice, as %q and %q", pick.Item.Link, first, pick.Item.Title)
		}
		seen[pick.Item.Link] = pick.Item.Title
	}
	// Four distinct articles exist between the two feeds, and all four should be drawn:
	// stepping over a duplicate must not cost the page a place.
	if len(picks) != 4 {
		t.Errorf("drew %d articles, want the 4 distinct ones", len(picks))
	}
}

// An article with no link is itself, not one of a crowd. Feeds that omit the link exist, and
// keying every one of them on the empty string would let a page show exactly one.
func TestArticlesWithNoLinkAreNotAllTheSameArticle(t *testing.T) {
	src := &Source{FeedID: "quiet", Priority: 50}
	for i := range 5 {
		src.Fresh = append(src.Fresh, &store.Item{
			ID: fmt.Sprintf("quiet-%d", i), FeedID: "quiet",
			GUID: fmt.Sprintf("quiet-%d", i), Title: fmt.Sprintf("Untitled %d", i),
		})
	}

	if picks := Select(sources(src), 10, 7); len(picks) != 5 {
		t.Errorf("drew %d link-less articles, want all 5", len(picks))
	}
}
