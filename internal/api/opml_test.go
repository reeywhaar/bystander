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

	tags := map[string]string{}
	for _, tag := range byTitle["New one"].Tags {
		tags[tag.Name] = tag.TagID
	}
	if tags["Art"] == "" {
		t.Error("Art is one of alice's tags and was not matched to it")
	}
	if id, named := tags["Woodworking"]; !named || id != "" {
		t.Errorf("Woodworking should be offered as a new tag, got id=%q named=%v", id, named)
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
	if plan.Feeds[0].Tags[0].TagID != "" {
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

// The plain list is what somebody pastes into a message, so it has to come back in.
func TestImportingThePlainList(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	first, second := newFeedServer(t, 3), newFeedServer(t, 2)
	shared := "The Example\n" + first.URL + "\nArt, News / World\n\n" +
		"Another\n" + second.URL + "\nArt"

	var plan struct {
		Feeds []previewFeed `json:"feeds"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/import/preview",
		map[string]string{"opml": shared}), http.StatusOK, &plan)

	if len(plan.Feeds) != 2 {
		t.Fatalf("%d feeds read from the plain list: %+v", len(plan.Feeds), plan.Feeds)
	}
	if plan.Feeds[0].Title != "The Example" {
		t.Errorf("title = %q", plan.Feeds[0].Title)
	}
	names := map[string]bool{}
	for _, tag := range plan.Feeds[0].Tags {
		names[tag.Name] = true
	}
	if !names["Art"] || !names["News / World"] {
		t.Errorf("tags = %+v, want Art and the nested one", plan.Feeds[0].Tags)
	}
}

// What this hands out has to be what it takes back.
func TestTheSharedListRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var art tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Art"}), http.StatusCreated, &art)
	feed := newFeedServer(t, 3)
	h.expect(h.do(http.MethodPost, "/api/feeds",
		map[string]any{"url": feed.URL, "tag_ids": []string{art.ID}}), http.StatusCreated, nil)

	// The shape ShareDialog builds client-side: title, address, tags.
	var subs []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &subs)
	shared := subs[0].Title + "\n" + subs[0].URL + "\nArt"

	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")

	var plan struct {
		Feeds []previewFeed `json:"feeds"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/import/preview",
		map[string]string{"opml": shared}), http.StatusOK, &plan)
	if len(plan.Feeds) != 1 {
		t.Fatalf("%d feeds: %+v", len(plan.Feeds), plan.Feeds)
	}
	if plan.Feeds[0].Tags[0].TagID != "" {
		t.Error("alice's tag was matched to one of bob's, and bob has none")
	}
}

// A list is somebody's judgement about a set of feeds, and how far back each is worth reading
// is part of that judgement — a news wire worth a day and a blog worth a year are not the same
// recommendation. So it travels with the list and arrives applied.
func TestASharedListBringsItsReachesWithIt(t *testing.T) {
	h := newHarness(t)
	daily := newFeedServer(t, 2)
	yearly := newFeedServer(t, 2)

	h.signIn(store.RoleUser, "alice")

	var quick, slow subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": daily.URL}),
		http.StatusCreated, &quick)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": yearly.URL}),
		http.StatusCreated, &slow)

	const day, year = 86400, 31536000
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+quick.ID,
		map[string]any{"article_window": day}), http.StatusOK, nil)
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+slow.ID,
		map[string]any{"article_window": year}), http.StatusOK, nil)

	file := h.exportOPML()
	if !strings.Contains(file, opml.ReachAttr) {
		t.Fatalf("the exported list says nothing about reach:\n%s", file)
	}

	// Somebody else opens it. The preview says what each feed would arrive with, before
	// anybody accepts anything.
	elsewhere := h.signInAsSomebodyElse("bob")

	var plan struct {
		Feeds []struct {
			Title   string `json:"title"`
			FeedURL string `json:"feed_url"`
			Reach   int    `json:"reach"`
		} `json:"feeds"`
	}
	res := h.doAs(elsewhere, http.MethodPost, "/api/feeds/import/preview", map[string]string{"opml": file})
	h.expect(res, http.StatusOK, &plan)
	if len(plan.Feeds) != 2 {
		t.Fatalf("%d feeds in the plan, want 2", len(plan.Feeds))
	}

	wanted := map[string]int{}
	for _, feed := range plan.Feeds {
		wanted[feed.FeedURL] = feed.Reach
	}

	selection := make([]map[string]any, 0, len(plan.Feeds))
	for _, feed := range plan.Feeds {
		selection = append(selection, map[string]any{
			"feed_url": feed.FeedURL,
			"title":    feed.Title,
			"reach":    feed.Reach,
		})
	}
	h.expect(h.doAs(elsewhere, http.MethodPost, "/api/feeds/import",
		map[string]any{"feeds": selection}), http.StatusOK, nil)

	var theirs []subscriptionBody
	h.expect(h.doAs(elsewhere, http.MethodGet, "/api/feeds", nil), http.StatusOK, &theirs)
	if len(theirs) != 2 {
		t.Fatalf("%d feeds followed, want 2", len(theirs))
	}
	for _, sub := range theirs {
		if want := wanted[sub.URL]; int(sub.ArticleWindow) != want {
			t.Errorf("%s arrived reaching back %ds, want %ds", sub.Title, sub.ArticleWindow, want)
		}
	}

	// And the two are different, so this is not passing by everything taking one default.
	if theirs[0].ArticleWindow == theirs[1].ArticleWindow {
		t.Errorf("both arrived at %ds; the list's own reaches were not carried", theirs[0].ArticleWindow)
	}
}

// Marking a feed read reaches further than the page in front of you, and that is the point.
//
// A page never offers an article somebody has already read, so marking a feed's backlog read
// means those articles are never drawn at all. That is what makes following a publisher again
// after a while start from now rather than from its archive — and it is the part a dialog has
// to say out loud, because it cannot be seen on screen.
func TestMarkingAFeedReadKeepsItsBacklogOffLaterPages(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 12)

	h.signIn(store.RoleUser, "alice")

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}),
		http.StatusCreated, &sub)

	// Nothing has been composed, so nothing has been shown — the whole feed is backlog.
	var before editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &before)
	if len(before.Items) != 0 {
		t.Fatalf("a page exists already with %d articles", len(before.Items))
	}

	var result struct {
		Marked int `json:"marked"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/"+sub.ID+"/read",
		map[string]string{"older_than": ""}), http.StatusOK, &result)
	if result.Marked == 0 {
		t.Fatal("marked nothing, though the feed's articles had never been shown")
	}

	// And now a page composed from it has nothing to draw.
	res := h.do(http.MethodPost, "/api/edition/regenerate", nil)
	if res.StatusCode < 400 {
		var after editionBody
		h.expect(res, http.StatusOK, &after)
		if len(after.Items) != 0 {
			t.Errorf("composed %d articles from a feed marked read", len(after.Items))
		}
	} else {
		res.Body.Close()
	}

	// It is in Recently read, which is the other half of what "read" means.
	var read []struct {
		Title string `json:"title"`
	}
	h.expect(h.do(http.MethodGet, "/api/read", nil), http.StatusOK, &read)
	if len(read) != result.Marked {
		t.Errorf("%d articles in Recently read, want the %d marked", len(read), result.Marked)
	}
}

// Only the feed asked for, and only what the caller follows.
func TestMarkingAFeedReadIsYourOwnFeedOnly(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)

	h.signIn(store.RoleUser, "alice")
	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}),
		http.StatusCreated, &sub)

	elsewhere := h.signInAsSomebodyElse("bob")
	res := h.doAs(elsewhere, http.MethodPost, "/api/feeds/"+sub.ID+"/read",
		map[string]string{"older_than": ""})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("marking somebody else's subscription = %d, want 404", res.StatusCode)
	}
	res.Body.Close()

	// And a span nobody offers is refused rather than quietly meaning everything.
	res = h.do(http.MethodPost, "/api/feeds/"+sub.ID+"/read",
		map[string]string{"older_than": "fortnight"})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown span = %d, want 400", res.StatusCode)
	}
	res.Body.Close()

	// Nothing was marked by either of those.
	var read []struct{}
	h.expect(h.do(http.MethodGet, "/api/read", nil), http.StatusOK, &read)
	if len(read) != 0 {
		t.Errorf("%d articles marked read by requests that should have failed", len(read))
	}
}
