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
	return &Source{FeedID: id, Priority: priority, Items: items}
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
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"a", "b", "c"}}}

	first := Select(buckets, src, nil, 20, 12345)
	second := Select(buckets, src, nil, 20, 12345)

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
	ids := []string{"loud"}
	for i := range 9 {
		ids = append(ids, fmt.Sprintf("quiet%d", i))
	}

	counts := map[string]int{}
	for seed := range 300 {
		list := []*Source{feed("loud", 90, 100)}
		for i := range 9 {
			list = append(list, feed(fmt.Sprintf("quiet%d", i), 10, 100))
		}
		buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: ids}}
		for _, pick := range Select(buckets, sources(list...), nil, 10, int64(seed)) {
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
		buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"on", "off"}}}
		for _, pick := range Select(buckets, src, nil, 20, int64(seed)) {
			if pick.Item.FeedID == "off" {
				t.Fatalf("seed %d drew from a feed at priority zero", seed)
			}
		}
	}
}

func TestAllZeroTerminates(t *testing.T) {
	src := sources(feed("a", 0, 10), feed("b", 0, 10))
	buckets := []Bucket{{TagID: "t1", Priority: 0, FeedIDs: []string{"a", "b"}}}
	if got := Select(buckets, src, nil, 20, 1); len(got) != 0 {
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
		buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"prolific", "occasional"}}}
		for _, pick := range Select(buckets, src, nil, size, int64(seed)) {
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
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"plenty", "trickle"}}}

	picks := Select(buckets, src, nil, size, 7)
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
		buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"loud", "quiet"}}}
		for _, pick := range Select(buckets, src, nil, size, int64(seed)) {
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

// A feed reachable through two tags is one queue: an article drawn through one must not
// be offered again through the other.
func TestNoDuplicatesAcrossBuckets(t *testing.T) {
	src := sources(feed("shared", 50, 30))
	buckets := []Bucket{
		{TagID: "art", Priority: 50, FeedIDs: []string{"shared"}},
		{TagID: "news", Priority: 50, FeedIDs: []string{"shared"}},
	}

	seen := map[string]bool{}
	for _, pick := range Select(buckets, src, nil, 20, 3) {
		if seen[pick.Item.ID] {
			t.Fatalf("article %q appears twice", pick.Item.ID)
		}
		seen[pick.Item.ID] = true
	}
}

// A short edition is the honest outcome of a dry pool, not something to pad.
func TestExhaustedPoolGivesAShortPage(t *testing.T) {
	src := sources(feed("a", 50, 3), feed("b", 50, 2))
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"a", "b"}}}

	got := Select(buckets, src, nil, 60, 9)
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
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: ids}}

	// Several seeds, because these are drawn: one page passing says little.
	for seed := int64(1); seed <= 12; seed++ {
		picks := Select(buckets, sources(list...), nil, 50, seed)
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
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: ids}}

	for seed := int64(1); seed <= 12; seed++ {
		for _, p := range Select(buckets, sources(list...), nil, 50, seed) {
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
	bare := &Source{FeedID: "bare", Priority: 50, Items: []*store.Item{
		{ID: "bare-0", FeedID: "bare", GUID: "bare-0", Title: "No picture, no words"},
	}}
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{"bare"}}}

	picks := Select(buckets, sources(bare), nil, 10, 1)
	if len(picks) != 1 {
		t.Fatalf("Select() returned %d articles, want 1", len(picks))
	}
	// Rank 0 would ordinarily be the lead.
	if picks[0].Slot != store.SlotBrief {
		t.Errorf("slot = %q, want brief", picks[0].Slot)
	}
}

func TestUntaggedBucketIsOrdinary(t *testing.T) {
	src := sources(feed("loose", 50, 10))
	// The empty TagID is what "no tag" looks like, and nothing treats it specially.
	buckets := []Bucket{{TagID: "", Priority: store.DefaultPriority, FeedIDs: []string{"loose"}}}

	if got := Select(buckets, src, nil, 5, 2); len(got) != 5 {
		t.Fatalf("Select() returned %d articles from the untagged bucket, want 5", len(got))
	}
}

// shaped is a feed whose every picture is the same measured shape.
func shaped(id string, priority, n, w, h int) *Source {
	src := feed(id, priority, n)
	for _, item := range src.Items {
		item.ImageWidth, item.ImageHeight = w, h
	}
	return src
}

// slotsFrom lays out several pages and tallies the widths every card came out at.
func slotsFrom(t *testing.T, src *Source, seeds int) map[store.Slot]int {
	t.Helper()
	tally := map[store.Slot]int{}
	for seed := int64(1); seed <= int64(seeds); seed++ {
		picks := Select([]Bucket{{TagID: "t1", Priority: 50, FeedIDs: []string{src.FeedID}}},
			sources(src), nil, 50, seed)
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
