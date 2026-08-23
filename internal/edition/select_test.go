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

func TestSlotsByRank(t *testing.T) {
	var list []*Source
	var ids []string
	for i := range 6 {
		id := fmt.Sprintf("f%d", i)
		list = append(list, feed(id, 50, 200))
		ids = append(ids, id)
	}
	buckets := []Bucket{{TagID: "t1", Priority: 50, FeedIDs: ids}}

	picks := Select(buckets, sources(list...), nil, 50, 11)
	if len(picks) < 10 {
		t.Fatalf("only %d articles selected; the rest of this test needs a full page", len(picks))
	}
	if picks[0].Slot != store.SlotLead {
		t.Errorf("rank 0 is %q, want lead", picks[0].Slot)
	}
	if picks[1].Slot != store.SlotFeature {
		t.Errorf("rank 1 is %q, want feature", picks[1].Slot)
	}
	if last := picks[len(picks)-1]; last.Slot != store.SlotStandard {
		t.Errorf("the last article is %q, want standard", last.Slot)
	}
	// Exactly one lead. A page with two is not a front page.
	leads := 0
	for _, p := range picks {
		if p.Slot == store.SlotLead {
			leads++
		}
	}
	if leads != 1 {
		t.Errorf("%d articles are laid out as the lead", leads)
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
