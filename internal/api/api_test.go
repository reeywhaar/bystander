package api

import (
	"net/http"
	"testing"
	"time"

	"bystander/internal/store"
)

func TestHealthz(t *testing.T) {
	h := newHarness(t)

	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	h.expect(h.do(http.MethodGet, "/healthz", nil), http.StatusOK, &body)
	if !body.OK {
		t.Error("healthz did not report itself healthy")
	}
}

// The whole product, in the order somebody meets it: a link becomes an account, an account
// adds a feed, a feed becomes a page.
func TestInviteToFrontPage(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 8)

	// An invitation reports its own state before anybody types a password into it.
	_, token, err := h.store.CreateInvite(t.Context(), store.RoleAdmin, "")
	if err != nil {
		t.Fatalf("CreateInvite(): %v", err)
	}
	var invite inviteBody
	h.expect(h.do(http.MethodGet, "/api/invites/"+token, nil), http.StatusOK, &invite)
	if !invite.Usable || invite.Role != string(store.RoleAdmin) {
		t.Fatalf("invite = %+v, want a usable admin invitation", invite)
	}

	// Accepting signs them in: they have just chosen a password, so asking for it again
	// proves nothing.
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "alice", "password": "correct-horse"}), http.StatusNoContent, nil)

	var me meBody
	h.expect(h.do(http.MethodGet, "/api/me", nil), http.StatusOK, &me)
	if me.Username != "alice" || me.Role != string(store.RoleAdmin) {
		t.Fatalf("me = %+v, want alice the administrator", me)
	}

	// A page before there is anything to put on it is a state, not an error.
	var empty editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("a brand new account already has %d articles", len(empty.Items))
	}

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, &sub)
	if sub.Title != "The Example" {
		t.Errorf("feed title = %q, want the publisher's", sub.Title)
	}
	if sub.Priority != store.DefaultPriority {
		t.Errorf("priority = %d, want the default of %d", sub.Priority, store.DefaultPriority)
	}

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) != 8 {
		t.Fatalf("the page holds %d articles, want the 8 the feed published", len(page.Items))
	}
	// The page opens with weight, but not always the same weight — the opener's width is
	// drawn from the three that are wider than a column.
	switch page.Items[0].Slot {
	case string(store.SlotLead), string(store.SlotWide), string(store.SlotFeature):
	default:
		t.Errorf("the page opens with %q, want something wider than a column", page.Items[0].Slot)
	}
	if page.Items[0].Feed.Title != "The Example" {
		t.Errorf("the card names its source as %q", page.Items[0].Feed.Title)
	}

	// Sanitizing happens at ingest, so what reaches the browser is already safe.
	for _, article := range page.Items {
		if contains(article.Summary, "<script") || contains(article.Summary, "alert(") {
			t.Fatalf("a script survived into a card: %q", article.Summary)
		}
	}
	if page.Items[0].ImageURL == "" {
		t.Error("no image was found for the lead, though the feed embedded one")
	}

	// A read mark greys a card in place; it does not move or remove it.
	first := page.Items[0].ID
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+first+"/read", nil), http.StatusNoContent, nil)

	var afterRead editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &afterRead)
	if len(afterRead.Items) != len(page.Items) {
		t.Fatalf("reading an article changed the page from %d articles to %d", len(page.Items), len(afterRead.Items))
	}
	if afterRead.Items[0].ID != first {
		t.Errorf("the read article moved from rank 0 to somewhere else")
	}
	if afterRead.Items[0].ReadAt == nil {
		t.Error("the article was not marked read")
	}

	h.expect(h.do(http.MethodDelete, "/api/edition/items/"+first+"/read", nil), http.StatusNoContent, nil)
	var afterUnread editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &afterUnread)
	if afterUnread.Items[0].ReadAt != nil {
		t.Error("the mark did not clear")
	}
}

// The button has to work more than once. Spending the pool on every press is what made it
// useless at exactly the moment somebody wants it — just after adding feeds, while tuning
// priorities and watching what changes.
func TestRegeneratingWorksRepeatedly(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 10)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPatch, "/api/pages/"+h.mainPage(), map[string]int{"edition_size": 10}), http.StatusOK, nil)

	var first editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &first)
	if len(first.Items) != 10 {
		t.Fatalf("the first page holds %d articles, want 10", len(first.Items))
	}

	// Four more presses, each of which has to produce a page rather than an apology.
	for press := range 4 {
		var again editionBody
		h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &again)
		if len(again.Items) != 10 {
			t.Fatalf("press %d gave %d articles, want 10", press+2, len(again.Items))
		}
		if again.ID == first.ID {
			t.Errorf("press %d returned the same page rather than a new one", press+2)
		}
	}
}

// A scheduled turn still spends what it shows, which is the product's promise. What a
// manual regeneration must not do is charge somebody for a day that did not pass.
func TestRegeneratingKeepsWhatWasNotRead(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 12)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPatch, "/api/pages/"+h.mainPage(), map[string]int{"edition_size": 10}), http.StatusOK, nil)

	var first editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &first)

	// One article read; the rest merely seen on a page nobody engaged with.
	read := first.Items[0]
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+read.ID+"/read", nil), http.StatusNoContent, nil)

	var second editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &second)

	for _, article := range second.Items {
		if article.ID == read.ID {
			t.Errorf("%q was read, and came back anyway", article.Title)
		}
	}
	// Twelve articles, one spent, a page of ten: the unread ones must have returned, or
	// this page could not have been filled.
	if len(second.Items) != 10 {
		t.Errorf("the re-rolled page holds %d articles, want 10", len(second.Items))
	}
}

// Feeds are global: two people following one URL cause one fetch.
func TestTwoSubscribersOneFetch(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 3)

	h.signIn(store.RoleAdmin, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	afterFirst := feed.hits

	// A second account, in the same process, following the same URL.
	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	// The second subscriber still causes a discovery fetch — the URL has to be checked
	// before it is accepted — but both share one feed row, so it is fetched once.
	feeds, err := h.store.FeedIDs(t.Context())
	if err != nil {
		t.Fatalf("FeedIDs(): %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("%d feed rows for one URL, want 1 (discovery fetches: %d)", len(feeds), feed.hits-afterFirst)
	}
}

func TestFollowingAFeedTwiceIsRefused(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 2)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusConflict, nil)
}

func TestAddingSomethingThatIsNotAFeed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	page := newPlainPage(t, `<!doctype html><html><head><title>No feed here</title></head><body>hi</body></html>`)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": page.URL}), http.StatusBadRequest, nil)
}

// Somebody pasting a site's home page should get its feed, not a refusal.
func TestAddingASiteFindsItsFeed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	feed := newFeedServer(t, 4)
	page := newPlainPage(t, `<!doctype html><html><head>`+
		`<link rel="alternate" type="application/rss+xml" href="`+feed.URL+`">`+
		`</head><body>a blog</body></html>`)

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": page.URL}), http.StatusCreated, &sub)
	if sub.Title != "The Example" {
		t.Errorf("subscribed to %q, want the feed the page named", sub.Title)
	}
}

func TestTagsShapeThePage(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var tag tagBody
	h.expect(h.do(http.MethodPost, "/api/tags",
		map[string]any{"name": "World News", "priority": 90}), http.StatusCreated, &tag)
	if tag.Priority != 90 {
		t.Fatalf("priority = %d, want 90", tag.Priority)
	}

	feed := newFeedServer(t, 5)
	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds",
		map[string]any{"url": feed.URL, "tag_ids": []string{tag.ID}}), http.StatusCreated, &sub)
	if len(sub.TagIDs) != 1 || sub.TagIDs[0] != tag.ID {
		t.Fatalf("tags = %v, want the one it was created with", sub.TagIDs)
	}

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) == 0 {
		t.Fatal("a tagged feed produced no page")
	}
}

// A tag inside itself would make the manage page recurse until it ran out of stack.
func TestTagCyclesAreRefused(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var parent, child tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "News"}), http.StatusCreated, &parent)
	h.expect(h.do(http.MethodPost, "/api/tags",
		map[string]any{"name": "World", "parent_id": parent.ID}), http.StatusCreated, &child)

	h.expect(h.do(http.MethodPatch, "/api/tags/"+parent.ID,
		map[string]any{"parent_id": child.ID}), http.StatusBadRequest, nil)
	h.expect(h.do(http.MethodPatch, "/api/tags/"+parent.ID,
		map[string]any{"parent_id": parent.ID}), http.StatusBadRequest, nil)
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// What somebody read outlives the page it was on. That is the point: last week's page is
// gone, and "what did I read last week" still has an answer.
func TestReadArticlesSurviveTheirPage(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 8)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)

	read := page.Items[0]
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+read.ID+"/read", nil), http.StatusNoContent, nil)

	var listed []readArticleBody
	h.expect(h.do(http.MethodGet, "/api/read", nil), http.StatusOK, &listed)
	if len(listed) != 1 {
		t.Fatalf("%d articles remembered, want 1", len(listed))
	}
	if listed[0].Title != read.Title {
		t.Errorf("remembered %q, want %q", listed[0].Title, read.Title)
	}
	if listed[0].Feed.Title != "The Example" {
		t.Errorf("the source is %q", listed[0].Feed.Title)
	}

	// A new page discards the old one and every read mark on it. The record must not go
	// with them.
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, nil)

	var afterwards []readArticleBody
	h.expect(h.do(http.MethodGet, "/api/read", nil), http.StatusOK, &afterwards)
	if len(afterwards) != 1 {
		t.Fatalf("%d articles remembered after regenerating, want 1", len(afterwards))
	}

	// It may come back — a page with room left over is filled from what has been seen
	// before rather than left short — but it must come back *read*. An article somebody
	// finished with last week reappearing as though it were new is the one thing a page
	// that repeats itself must not do.
	var next editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &next)
	for _, article := range next.Items {
		if article.ID == read.ID && article.ReadAt == nil {
			t.Errorf("%q was read, and came back looking new", article.Title)
		}
	}
}

// Unreading is a correction, not a second event: it should leave no trace behind.
func TestUnreadingForgets(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	id := page.Items[0].ID

	h.expect(h.do(http.MethodPut, "/api/edition/items/"+id+"/read", nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodDelete, "/api/edition/items/"+id+"/read", nil), http.StatusNoContent, nil)

	var listed []readArticleBody
	h.expect(h.do(http.MethodGet, "/api/read", nil), http.StatusOK, &listed)
	if len(listed) != 0 {
		t.Errorf("%d articles remembered after unreading, want none", len(listed))
	}
}

// One person's reading is their own.
func TestReadArticlesAreScopedToTheirReader(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 4)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	h.expect(h.do(http.MethodPut, "/api/edition/items/"+page.Items[0].ID+"/read", nil), http.StatusNoContent, nil)

	h.expect(h.do(http.MethodPost, "/api/logout", nil), http.StatusNoContent, nil)
	h.signIn(store.RoleUser, "bob")

	var bobs []readArticleBody
	h.expect(h.do(http.MethodGet, "/api/read", nil), http.StatusOK, &bobs)
	if len(bobs) != 0 {
		t.Errorf("bob can see %d of alice's read articles", len(bobs))
	}
}

// A site usually names more than one feed. Handing somebody whichever came first in the
// markup is how they end up subscribed to a comments feed they did not want.
func TestDiscoverListsEveryFeedASiteNames(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	posts := newFeedServer(t, 3)
	comments := newFeedServer(t, 2)
	site := newSiteWithFeeds(t, map[string]string{
		"Posts":    posts.URL,
		"Comments": comments.URL,
	})

	var found struct {
		Candidates []previewFeed `json:"candidates"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/discover",
		map[string]string{"url": site.URL}), http.StatusOK, &found)

	if len(found.Candidates) != 2 {
		t.Fatalf("%d candidates, want 2: %+v", len(found.Candidates), found.Candidates)
	}
	titles := map[string]bool{}
	for _, candidate := range found.Candidates {
		titles[candidate.Title] = true
		if candidate.FeedURL == "" {
			t.Error("a candidate has no URL to subscribe to")
		}
	}
	// The <link title> is what distinguishes them, and it is the only thing that can
	// without fetching each one.
	if !titles["Posts"] || !titles["Comments"] {
		t.Errorf("candidate titles = %v, want the ones the page gave", titles)
	}

	// Nothing was subscribed to: discovery only looks.
	var subs []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &subs)
	if len(subs) != 0 {
		t.Errorf("discovery subscribed to %d feeds", len(subs))
	}
}

// A feed URL is a feed, and discovering it must not cost a second round trip to find out.
func TestDiscoverOnAFeedUrl(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	feed := newFeedServer(t, 3)

	var found struct {
		Candidates []previewFeed `json:"candidates"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/discover",
		map[string]string{"url": feed.URL}), http.StatusOK, &found)

	if len(found.Candidates) != 1 {
		t.Fatalf("%d candidates for a feed URL, want 1", len(found.Candidates))
	}
	if found.Candidates[0].Title != "The Example" {
		t.Errorf("title = %q, want the feed's own", found.Candidates[0].Title)
	}
}

func TestDiscoverOnAPageWithNoFeeds(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	page := newPlainPage(t, `<!doctype html><html><head><title>Nothing here</title></head><body>hi</body></html>`)

	var refusal errorBody
	h.expect(h.do(http.MethodPost, "/api/feeds/discover",
		map[string]string{"url": page.URL}), http.StatusBadRequest, &refusal)
	if !contains(refusal.Error, "does not offer a feed") {
		t.Errorf("refusal = %q", refusal.Error)
	}
}

// The same page declaring one feed twice — once for the browser, once for a reader
// extension — is naming one feed.
func TestDiscoverDeduplicates(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	posts := newFeedServer(t, 3)
	site := newPlainPage(t, `<!doctype html><html><head>`+
		`<link rel="alternate" type="application/rss+xml" title="Posts" href="`+posts.URL+`">`+
		`<link rel="alternate" type="application/atom+xml" title="Posts (Atom)" href="`+posts.URL+`">`+
		`</head><body>x</body></html>`)

	var found struct {
		Candidates []previewFeed `json:"candidates"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/discover",
		map[string]string{"url": site.URL}), http.StatusOK, &found)
	if len(found.Candidates) != 1 {
		t.Fatalf("%d candidates, want 1 after deduplication", len(found.Candidates))
	}
}

// A site that declares no feed and serves one anyway is the ordinary case for anything
// rendered client-side — Reddit's front page is a script shell while reddit.com/.rss is a
// perfectly good Atom feed. Refusing those would refuse a large part of the web.
func TestDiscoverGuessesTheUsualAddresses(t *testing.T) {
	for _, path := range []string{"/.rss", "/feed", "/index.xml", "/atom.xml"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t)
			h.signIn(store.RoleUser, "alice")
			site := newSilentSite(t, path, 3)

			var found struct {
				Candidates []previewFeed `json:"candidates"`
			}
			h.expect(h.do(http.MethodPost, "/api/feeds/discover",
				map[string]string{"url": site.URL}), http.StatusOK, &found)

			if len(found.Candidates) != 1 {
				t.Fatalf("%d candidates, want the one at %s: %+v", len(found.Candidates), path, found.Candidates)
			}
			if !contains(found.Candidates[0].FeedURL, path) {
				t.Errorf("found %q, want the feed at %s", found.Candidates[0].FeedURL, path)
			}
			if found.Candidates[0].Title != "The Example" {
				t.Errorf("title = %q, want the feed's own", found.Candidates[0].Title)
			}
		})
	}
}

// Guessing must not turn every 200 into a feed. A site that answers its home page for
// anything — a soft 404 — offers nothing, and saying so is the honest answer.
func TestDiscoverDoesNotInventFeeds(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	// Answers 200 with HTML for every path, including the guessed ones.
	soft404 := newPlainPage(t, `<!doctype html><html><head><title>Everything is fine</title></head><body>hi</body></html>`)

	h.expect(h.do(http.MethodPost, "/api/feeds/discover",
		map[string]string{"url": soft404.URL}), http.StatusBadRequest, nil)
}

// A declared feed is the site's own answer, so nothing should be guessed at on top of it.
func TestDeclaredFeedsAreNotSupplementedByGuesses(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	posts := newFeedServer(t, 3)
	site := newSiteWithFeeds(t, map[string]string{"Posts": posts.URL})

	var found struct {
		Candidates []previewFeed `json:"candidates"`
	}
	h.expect(h.do(http.MethodPost, "/api/feeds/discover",
		map[string]string{"url": site.URL}), http.StatusOK, &found)
	if len(found.Candidates) != 1 {
		t.Fatalf("%d candidates, want only the declared one", len(found.Candidates))
	}
}

// A front page is about what is going on, so an article older than the window is not a
// candidate at all — and the window belongs to the feed, not to the reader.
func TestArticlesOlderThanTheWindowAreNotPicked(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	feed := newFeedServer(t, 6)

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, &sub)
	if sub.ArticleWindow != int64(store.DefaultArticleWindow.Seconds()) {
		t.Errorf("a new feed's window is %ds, want a week", sub.ArticleWindow)
	}

	// The harness publishes within the last few hours, so a week takes everything.
	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) != 6 {
		t.Fatalf("%d articles with a week's window, want all 6", len(page.Items))
	}

	// Age them past every window by moving the articles back a fortnight.
	if _, err := h.store.Derived().ExecContext(t.Context(),
		`UPDATE items SET published_at = published_at - ?`, int64((14 * 24 * time.Hour).Seconds())); err != nil {
		t.Fatalf("age the articles: %v", err)
	}
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+sub.ID,
		map[string]int64{"article_window": 86400}), http.StatusOK, nil)

	res := h.do(http.MethodPost, "/api/edition/regenerate", nil)
	res.Body.Close()
	if res.StatusCode == http.StatusOK {
		var after editionBody
		h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &after)
		for _, article := range after.Items {
			t.Errorf("%q is a fortnight old and reached a page with a day's window", article.Title)
		}
	}

	// Widening this feed's window brings them back — they were never thrown away, they
	// were out of reach.
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+sub.ID,
		map[string]int64{"article_window": 2592000}), http.StatusOK, nil)

	var wider editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &wider)
	if len(wider.Items) == 0 {
		t.Error("a month's window found nothing, though the articles are a fortnight old")
	}
}

// One feed reaching back further must not drag another with it.
func TestEachFeedKeepsItsOwnWindow(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	fresh, stale := newFeedServer(t, 4), newFeedServer(t, 4)
	var freshSub, staleSub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": fresh.URL}), http.StatusCreated, &freshSub)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": stale.URL}), http.StatusCreated, &staleSub)

	// Everything is a fortnight old.
	if _, err := h.store.Derived().ExecContext(t.Context(),
		`UPDATE items SET published_at = published_at - ?`, int64((14 * 24 * time.Hour).Seconds())); err != nil {
		t.Fatalf("age the articles: %v", err)
	}

	// One feed reaches back a month, the other keeps its week.
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+staleSub.ID,
		map[string]int64{"article_window": 2592000}), http.StatusOK, nil)

	var page editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &page)
	if len(page.Items) == 0 {
		t.Fatal("the page is empty; the widened feed should have filled it")
	}
	for _, article := range page.Items {
		if article.Feed.ID == freshSub.ID {
			t.Errorf("%q came from the feed still on a week", article.Title)
		}
	}
}

func TestTheWindowIsAClosedSet(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	feed := newFeedServer(t, 2)
	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, &sub)

	for _, bad := range []int64{1, 3600, 999999} {
		h.expect(h.do(http.MethodPatch, "/api/feeds/"+sub.ID,
			map[string]int64{"article_window": bad}), http.StatusBadRequest, nil)
	}
	for _, good := range []int64{0, 86400, 604800, 1209600, 2592000, 31536000} {
		h.expect(h.do(http.MethodPatch, "/api/feeds/"+sub.ID,
			map[string]int64{"article_window": good}), http.StatusOK, nil)
	}
}

// A feed can be given a better name, and the publisher's own has to survive so it can be
// shown and put back.
func TestRenamingAFeed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")
	feed := newFeedServer(t, 3)

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, &sub)
	if sub.Title != "The Example" || sub.FeedTitle != "The Example" {
		t.Fatalf("before renaming: title=%q feed_title=%q", sub.Title, sub.FeedTitle)
	}

	var renamed subscriptionBody
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+sub.ID,
		map[string]string{"title_override": "Something bearable"}), http.StatusOK, &renamed)

	if renamed.Title != "Something bearable" {
		t.Errorf("title = %q, want the new name", renamed.Title)
	}
	if renamed.FeedTitle != "The Example" {
		t.Errorf("feed_title = %q, want the publisher's own to survive", renamed.FeedTitle)
	}

	// Emptying it puts the publisher's name back rather than leaving a blank.
	var reset subscriptionBody
	h.expect(h.do(http.MethodPatch, "/api/feeds/"+sub.ID,
		map[string]string{"title_override": ""}), http.StatusOK, &reset)
	if reset.Title != "The Example" || reset.TitleOverride != "" {
		t.Errorf("after clearing: title=%q override=%q", reset.Title, reset.TitleOverride)
	}
}
