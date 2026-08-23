package api

import (
	"net/http"
	"strings"
	"testing"

	"bystander/internal/opml"
	"bystander/internal/store"
)

// exportOPML asks for a subscription list and returns it as text, the way the interface
// receives it.
func (h *harness) exportOPML(ids ...string) string {
	h.t.Helper()

	var body struct {
		OPML     string `json:"opml"`
		Filename string `json:"filename"`
		Count    int    `json:"count"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/export", map[string]any{"ids": ids}), http.StatusOK, &body)
	if body.Filename == "" {
		h.t.Error("the export suggests no filename")
	}
	return body.OPML
}

func TestExportCarriesTagsAsCategories(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	// A nested tag, so the path has something to say.
	var news, world, art tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "News", "priority": 70}), http.StatusCreated, &news)
	h.expect(h.do(http.MethodPost, "/api/tags",
		map[string]any{"name": "World", "parent_id": news.ID}), http.StatusCreated, &world)
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Art"}), http.StatusCreated, &art)

	feed := newFeedServer(t, 3)
	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds",
		map[string]any{"url": feed.URL, "tag_ids": []string{world.ID, art.ID}, "priority": 90}),
		http.StatusCreated, &sub)

	exported := h.exportOPML()

	// Flat: several tags on one feed is exactly what folders cannot express.
	if n := strings.Count(exported, "<outline"); n != 1 {
		t.Errorf("%d outlines for one feed; the list should be flat:\n%s", n, exported)
	}
	if !strings.Contains(exported, `type="rss"`) || !strings.Contains(exported, feed.URL) {
		t.Errorf("the feed is missing from:\n%s", exported)
	}

	doc, err := opml.Decode(strings.NewReader(exported))
	if err != nil {
		t.Fatalf("our own export does not parse: %v\n%s", err, exported)
	}
	if len(doc.Feeds) != 1 {
		t.Fatalf("%d feeds came back", len(doc.Feeds))
	}

	paths := map[string]bool{}
	for _, category := range doc.Feeds[0].Categories {
		paths[strings.Join(category, "/")] = true
	}
	if !paths["News/World"] {
		t.Errorf("categories = %v, want the nested tag as a path", doc.Feeds[0].Categories)
	}
	if !paths["Art"] {
		t.Errorf("categories = %v, want both tags", doc.Feeds[0].Categories)
	}
	if doc.Feeds[0].Priority != 90 {
		t.Errorf("priority = %d, want 90", doc.Feeds[0].Priority)
	}
}

func TestExportSelectsWhatWasAskedFor(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	first := newFeedServer(t, 2)
	second := newFeedServer(t, 2)
	var keep, drop subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": first.URL}), http.StatusCreated, &keep)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": second.URL}), http.StatusCreated, &drop)

	if n := strings.Count(h.exportOPML(), "<outline"); n != 2 {
		t.Errorf("an empty selection exported %d feeds, want everything", n)
	}

	chosen := h.exportOPML(keep.ID)
	if n := strings.Count(chosen, "<outline"); n != 1 {
		t.Fatalf("a selection of one exported %d feeds", n)
	}
	if !strings.Contains(chosen, first.URL) || strings.Contains(chosen, second.URL) {
		t.Errorf("the wrong feed was exported:\n%s", chosen)
	}
}

// The two questions worth asking before somebody else's list lands in your account: what
// have I got already, and which of these tags are mine.
func TestPreviewSaysWhatIsAlreadyHereAndWhichTagsAreMine(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	mine, theirs := newFeedServer(t, 2), newFeedServer(t, 2)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": mine.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Art"}), http.StatusCreated, nil)

	shared := `<?xml version="1.0"?><opml version="2.0"><body>` +
		`<outline type="rss" text="Already" xmlUrl="` + mine.URL + `" category="/Art"/>` +
		`<outline type="rss" text="New one" xmlUrl="` + theirs.URL + `" category="/Art,/Woodworking"/>` +
		`</body></opml>`

	var plan struct {
		Feeds []previewFeed `json:"feeds"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/import/preview",
		map[string]string{"opml": shared}), http.StatusOK, &plan)

	if len(plan.Feeds) != 2 {
		t.Fatalf("%d feeds in the plan, want 2", len(plan.Feeds))
	}

	byTitle := map[string]previewFeed{}
	for _, feed := range plan.Feeds {
		byTitle[feed.Title] = feed
	}
	if !byTitle["Already"].AlreadySubscribed {
		t.Error("a feed already followed is not marked as such")
	}
	if byTitle["New one"].AlreadySubscribed {
		t.Error("a feed not followed is marked as already subscribed")
	}

	// Nothing was imported: a preview only looks.
	var subs []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &subs)
	if len(subs) != 1 {
		t.Errorf("the preview subscribed to something: %d feeds", len(subs))
	}

	tags := map[string]bool{}
	for _, tag := range byTitle["New one"].Tags {
		tags[tag.Name] = tag.Existing
	}
	if !tags["Art"] {
		t.Error("Art is one of alice's tags and was not matched to it")
	}
	if existing, named := tags["Woodworking"]; !named || existing {
		t.Errorf("Woodworking should be offered as a new tag, got existing=%v named=%v", existing, named)
	}
}

// Unticking a feed is not sending it, and unticking a tag is not sending it either.
func TestImportTakesOnlyWhatWasSent(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	wanted, unwanted := newFeedServer(t, 2), newFeedServer(t, 2)

	var result importResult
	h.expect(h.do(http.MethodPost, "/api/feeds/import", map[string]any{
		"feeds": []map[string]any{{
			"feed_url":  wanted.URL,
			"title":     "Kept",
			"priority":  80,
			"tag_paths": [][]string{{"News", "World"}},
		}},
	}), http.StatusOK, &result)

	if result.Added != 1 {
		t.Fatalf("added %d, want 1 (failed: %+v)", result.Added, result.Failed)
	}

	var subs []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &subs)
	if len(subs) != 1 {
		t.Fatalf("%d subscriptions, want only the one that was sent", len(subs))
	}
	if subs[0].Priority != 80 {
		t.Errorf("priority = %d, want the 80 that was sent", subs[0].Priority)
	}
	if strings.Contains(h.exportOPML(), unwanted.URL) {
		t.Error("a feed that was never sent was imported anyway")
	}

	// The whole path was created, and reported as new.
	var tags []tagBody
	h.expect(h.do(http.MethodGet, "/api/tags", nil), http.StatusOK, &tags)
	if len(tags) != 2 {
		t.Fatalf("%d tags created, want News and World", len(tags))
	}
	if len(result.Tags) == 0 {
		t.Error("nothing was reported as newly created")
	}
}

// Importing the same list twice is what happens when somebody re-sends it. It must not
// double anything or read as a failure.
func TestImportingTwiceIsQuiet(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	feed := newFeedServer(t, 2)

	payload := map[string]any{
		"feeds": []map[string]any{{
			"feed_url":  feed.URL,
			"title":     "Once",
			"tag_paths": [][]string{{"Art"}},
		}},
	}

	var first, second importResult
	h.expect(h.do(http.MethodPost, "/api/feeds/import", payload), http.StatusOK, &first)
	h.expect(h.do(http.MethodPost, "/api/feeds/import", payload), http.StatusOK, &second)

	if first.Added != 1 || second.Added != 0 || second.Skipped != 1 {
		t.Errorf("first = %+v, second = %+v; want one added then one skipped", first, second)
	}
	if len(second.Failed) != 0 {
		t.Errorf("re-importing reported failures: %+v", second.Failed)
	}

	var tags []tagBody
	h.expect(h.do(http.MethodGet, "/api/tags", nil), http.StatusOK, &tags)
	if len(tags) != 1 {
		t.Errorf("%d tags after importing twice, want 1", len(tags))
	}
}

// The list somebody exports has to be the list somebody else can import.
func TestAnExportImportsIntoAnotherAccount(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var art tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Art"}), http.StatusCreated, &art)
	feed := newFeedServer(t, 3)
	h.expect(h.do(http.MethodPost, "/api/feeds",
		map[string]any{"url": feed.URL, "tag_ids": []string{art.ID}}), http.StatusCreated, nil)

	shared := h.exportOPML()

	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")

	var plan struct {
		Feeds []previewFeed `json:"feeds"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/import/preview",
		map[string]string{"opml": shared}), http.StatusOK, &plan)
	if len(plan.Feeds) != 1 {
		t.Fatalf("%d feeds in bob's plan", len(plan.Feeds))
	}
	if plan.Feeds[0].Tags[0].Existing {
		t.Error("alice's tag is marked as one bob already has")
	}

	send := []map[string]any{{
		"feed_url":  plan.Feeds[0].FeedURL,
		"title":     plan.Feeds[0].Title,
		"site_url":  plan.Feeds[0].SiteURL,
		"priority":  plan.Feeds[0].Priority,
		"tag_paths": [][]string{plan.Feeds[0].Tags[0].Path},
	}}
	var result importResult
	h.expect(h.do(http.MethodPost, "/api/feeds/import",
		map[string]any{"feeds": send}), http.StatusOK, &result)
	if result.Added != 1 {
		t.Fatalf("bob added %d feeds: %+v", result.Added, result.Failed)
	}

	var subs []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &subs)
	if len(subs) != 1 || len(subs[0].TagIDs) != 1 {
		t.Fatalf("bob has %+v", subs)
	}

	var bobsTags []tagBody
	h.expect(h.do(http.MethodGet, "/api/tags", nil), http.StatusOK, &bobsTags)
	if len(bobsTags) != 1 || bobsTags[0].Name != "Art" {
		t.Errorf("bob's tags = %+v, want the one from the list", bobsTags)
	}
}

func TestImportRejectsRubbish(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	for _, bad := range []string{"", "not opml", "<html><body>hi</body></html>"} {
		h.expect(h.do(http.MethodPost, "/api/feeds/import/preview",
			map[string]string{"opml": bad}), http.StatusBadRequest, nil)
	}

	// Valid OPML with nothing in it is a different mistake, and says so.
	var refusal errorBody
	h.expect(h.do(http.MethodPost, "/api/feeds/import/preview",
		map[string]string{"opml": `<opml version="2.0"><body></body></opml>`}), http.StatusBadRequest, &refusal)
	if !contains(refusal.Error, "no feeds") {
		t.Errorf("refusal = %q", refusal.Error)
	}
}

// One person's list is their own.
func TestExportIsScopedToItsOwner(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	feed := newFeedServer(t, 2)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")

	if strings.Contains(h.exportOPML(), feed.URL) {
		t.Error("bob's export carries alice's feed")
	}
}
