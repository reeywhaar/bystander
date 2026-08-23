package api

import (
	"net/http"
	"strings"
	"testing"

	"bystander/internal/store"
)

// A link carries the same list a file would, to the same picker.
func TestASharedLinkOffersWhatWasShared(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	feed := newFeedServer(t, 3)

	var sub subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}),
		http.StatusCreated, &sub)

	var link shareBody
	h.expect(h.do(http.MethodPost, "/api/shares", map[string]any{"ids": []string{}}),
		http.StatusCreated, &link)
	if link.Count != 1 {
		t.Fatalf("count = %d, want the one feed", link.Count)
	}
	// Absolute, because it is about to leave this browser for a message or a camera.
	if !strings.HasPrefix(link.URL, h.cfg.PublicURL.String()) {
		t.Errorf("url = %q, want it to start with the public address", link.URL)
	}
	if !strings.Contains(link.URL, "/share/") {
		t.Errorf("url = %q", link.URL)
	}

	// Somebody else, on this instance.
	_, token, err := h.store.CreateInvite(t.Context(), store.RoleUser, "")
	if err != nil {
		t.Fatal(err)
	}
	h.expect(h.do(http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"username": "bob", "password": harnessPassword}), http.StatusNoContent, nil)

	var got sharedListBody
	h.expect(h.do(http.MethodGet, sharePath(link.URL), nil), http.StatusOK, &got)
	if got.From != "alice" {
		t.Errorf("from = %q; a list of feeds with no name on it is a list of URLs", got.From)
	}
	if len(got.Feeds) != 1 {
		t.Fatalf("%d feeds in the link, want one", len(got.Feeds))
	}
	if got.Feeds[0].FeedURL != sub.URL {
		t.Errorf("feed url = %q, want the canonical %q", got.Feeds[0].FeedURL, sub.URL)
	}
	// Bob does not follow it, and the picker has to say so — that flag is what makes a
	// list of somebody else's feeds legible.
	if got.Feeds[0].AlreadySubscribed {
		t.Error("bob is not subscribed to it, but the link says he is")
	}

	// Opening a link subscribes nobody to anything.
	var mine []subscriptionBody
	h.expect(h.do(http.MethodGet, "/api/feeds", nil), http.StatusOK, &mine)
	if len(mine) != 0 {
		t.Errorf("opening a link added %d feeds", len(mine))
	}
}

// A share is a snapshot, not a window onto what somebody currently reads.
func TestASharedLinkKeepsSayingWhatItSaid(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	first, second := newFeedServer(t, 2), newFeedServer(t, 2)

	var one, two subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": first.URL}),
		http.StatusCreated, &one)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": second.URL}),
		http.StatusCreated, &two)

	var link shareBody
	h.expect(h.do(http.MethodPost, "/api/shares", map[string]any{"ids": []string{}}),
		http.StatusCreated, &link)

	// Alice changes her mind about one of them after sharing.
	h.expect(h.do(http.MethodDelete, "/api/feeds/"+two.ID, nil), http.StatusNoContent, nil)

	var got sharedListBody
	h.expect(h.do(http.MethodGet, sharePath(link.URL), nil), http.StatusOK, &got)
	if len(got.Feeds) != 2 {
		t.Errorf("%d feeds, want the two that were shared: unfollowing something is not a "+
			"reason for somebody else's link to change under them", len(got.Feeds))
	}
}

func TestASharedLinkCanBeNarrowedToSomeFeeds(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")
	first, second := newFeedServer(t, 2), newFeedServer(t, 2)

	var one, two subscriptionBody
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": first.URL}),
		http.StatusCreated, &one)
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": second.URL}),
		http.StatusCreated, &two)

	var link shareBody
	h.expect(h.do(http.MethodPost, "/api/shares", map[string]any{"ids": []string{one.ID}}),
		http.StatusCreated, &link)
	if link.Count != 1 {
		t.Fatalf("count = %d, want just the one named", link.Count)
	}

	var got sharedListBody
	h.expect(h.do(http.MethodGet, sharePath(link.URL), nil), http.StatusOK, &got)
	if len(got.Feeds) != 1 || got.Feeds[0].FeedURL != one.URL {
		t.Errorf("the link carried %d feeds: %+v", len(got.Feeds), got.Feeds)
	}
}

func TestSharingNothingIsRefused(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	// No feeds at all. A link to an empty list is a link that wastes somebody's time.
	h.expect(h.do(http.MethodPost, "/api/shares", map[string]any{"ids": []string{}}),
		http.StatusBadRequest, nil)
}

// A link nobody made is a 404, the same answer an expired one gets — see the store test for
// the expiry half, which cannot live here: a week's clock skew expires the session doing the
// asking as readily as it expires the link.
func TestAnUnknownLinkIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "alice")

	h.expect(h.do(http.MethodGet, "/api/shares/not-a-real-token", nil), http.StatusNotFound, nil)
}

func TestSharesNeedASession(t *testing.T) {
	h := newHarness(t)

	// A list of what somebody reads is handed to another person on this instance, not
	// published to whoever finds the URL.
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/api/shares"},
		{http.MethodGet, "/api/shares/whatever"},
	} {
		res := h.do(call.method, call.path, map[string]any{})
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", call.method, call.path, res.StatusCode)
		}
		res.Body.Close()
	}
}

// sharePath is the API path for a link, given the URL somebody would open.
func sharePath(url string) string {
	_, token, _ := strings.Cut(url, "/share/")
	return "/api/shares/" + token
}
