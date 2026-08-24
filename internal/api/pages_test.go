package api

import (
	"net/http"
	"testing"

	"bystander/internal/store"
)

func TestEveryAccountHasOnePageToBeginWith(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var pages []pageBody
	h.expect(h.do(http.MethodGet, "/api/pages", nil), http.StatusOK, &pages)

	if len(pages) != 1 {
		t.Fatalf("%d pages, want 1", len(pages))
	}
	if !pages[0].IsMain || pages[0].Slug != "" {
		t.Errorf("page = %+v, want the main page at the root", pages[0])
	}
	// The strip needs these to draw the controls beside each tab.
	if pages[0].EditionSize == 0 || pages[0].EditionInterval == 0 {
		t.Errorf("page = %+v, want its cadence and size", pages[0])
	}
	// Never null, so the interface can read them without checking.
	if pages[0].TagIDs == nil || pages[0].FeedIDs == nil {
		t.Errorf("page = %+v, want empty lists rather than nulls", pages[0])
	}
}

func TestAPageIsCreatedAndListed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var made pageBody
	h.expect(h.do(http.MethodPost, "/api/pages",
		map[string]any{"name": "Finances", "slug": "finances"}), http.StatusCreated, &made)
	if made.IsMain || made.Slug != "finances" {
		t.Errorf("page = %+v, want an ordinary page at /f/finances", made)
	}

	var pages []pageBody
	h.expect(h.do(http.MethodGet, "/api/pages", nil), http.StatusOK, &pages)
	if len(pages) != 2 || !pages[0].IsMain || pages[1].ID != made.ID {
		t.Errorf("pages = %+v, want the main page first", pages)
	}

	// Addressable by slug as well as by id, because the interface has the slug in the URL.
	for _, ref := range []string{made.ID, "finances"} {
		var got pageBody
		h.expect(h.do(http.MethodGet, "/api/pages/"+ref, nil), http.StatusOK, &got)
		if got.ID != made.ID {
			t.Errorf("GET /api/pages/%s = %s, want %s", ref, got.ID, made.ID)
		}
	}
}

func TestAPageIsSavedInOneRequest(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var tag struct {
		ID string `json:"id"`
	}
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Finance"}),
		http.StatusCreated, &tag)

	var page pageBody
	h.expect(h.do(http.MethodPost, "/api/pages",
		map[string]any{"name": "Finances", "slug": "finances"}), http.StatusCreated, &page)

	var saved pageBody
	h.expect(h.do(http.MethodPatch, "/api/pages/"+page.ID, map[string]any{
		"tag_filter":       "including",
		"tag_ids":          []string{tag.ID},
		"edition_interval": 3600,
		"edition_size":     20,
		"max_article_age":  86400,
	}), http.StatusOK, &saved)

	if saved.TagFilter != "including" || len(saved.TagIDs) != 1 || saved.TagIDs[0] != tag.ID {
		t.Errorf("page = %+v, want it including one tag", saved)
	}
	if saved.EditionInterval != 3600 || saved.EditionSize != 20 || saved.MaxArticleAge != 86400 {
		t.Errorf("page = %+v, want hourly, twenty articles, a day's window", saved)
	}
}

// Sending [] and leaving the field out are different requests, and the interface relies on the
// difference: it saves a whole page in one PATCH, and it also nudges one control at a time.
func TestAnAbsentListIsNotAnEmptyOne(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var tag struct {
		ID string `json:"id"`
	}
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Finance"}),
		http.StatusCreated, &tag)

	var page pageBody
	h.expect(h.do(http.MethodPost, "/api/pages",
		map[string]any{"name": "Finances", "slug": "finances"}), http.StatusCreated, &page)
	h.expect(h.do(http.MethodPatch, "/api/pages/"+page.ID, map[string]any{
		"tag_filter": "including", "tag_ids": []string{tag.ID},
	}), http.StatusOK, nil)

	// Something else entirely; the tag list is not mentioned and must survive.
	var after pageBody
	h.expect(h.do(http.MethodPatch, "/api/pages/"+page.ID,
		map[string]any{"edition_size": 30}), http.StatusOK, &after)
	if len(after.TagIDs) != 1 {
		t.Errorf("tags = %v, want the list left alone by a request that did not mention it", after.TagIDs)
	}

	// Named and empty means empty.
	h.expect(h.do(http.MethodPatch, "/api/pages/"+page.ID,
		map[string]any{"tag_ids": []string{}}), http.StatusOK, &after)
	if len(after.TagIDs) != 0 {
		t.Errorf("tags = %v, want the list cleared when it was named", after.TagIDs)
	}
}

func TestTheMainPageIsFixedThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var pages []pageBody
	h.expect(h.do(http.MethodGet, "/api/pages", nil), http.StatusOK, &pages)
	main := pages[0].ID

	res := h.do(http.MethodPatch, "/api/pages/"+main, map[string]any{"name": "Something else"})
	if res.StatusCode < 400 {
		t.Errorf("PATCH name on the main page = %d, want a refusal", res.StatusCode)
	}
	res = h.do(http.MethodDelete, "/api/pages/"+main, nil)
	if res.StatusCode < 400 {
		t.Errorf("DELETE the main page = %d, want a refusal", res.StatusCode)
	}

	// Its cadence is not fixed, only its identity.
	h.expect(h.do(http.MethodPatch, "/api/pages/"+main,
		map[string]any{"edition_size": 25}), http.StatusOK, nil)
}

func TestAPageIsRemoved(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var page pageBody
	h.expect(h.do(http.MethodPost, "/api/pages",
		map[string]any{"name": "Finances", "slug": "finances"}), http.StatusCreated, &page)
	h.expect(h.do(http.MethodDelete, "/api/pages/"+page.ID, nil), http.StatusNoContent, nil)

	var pages []pageBody
	h.expect(h.do(http.MethodGet, "/api/pages", nil), http.StatusOK, &pages)
	if len(pages) != 1 {
		t.Errorf("%d pages after removing one, want 1", len(pages))
	}
}

// Somebody else's page is not found rather than forbidden: whether a stranger keeps a page
// called "finances" is not this caller's business either way.
func TestAPageIsYourOwnOnly(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var page pageBody
	h.expect(h.do(http.MethodPost, "/api/pages",
		map[string]any{"name": "Finances", "slug": "finances"}), http.StatusCreated, &page)

	elsewhere := h.signInAsSomebodyElse("bob")
	for _, probe := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/pages/" + page.ID, nil},
		{http.MethodPatch, "/api/pages/" + page.ID, map[string]any{"edition_size": 11}},
		{http.MethodDelete, "/api/pages/" + page.ID, nil},
		{http.MethodGet, "/api/edition?page=" + page.ID, nil},
	} {
		res := h.doAs(elsewhere, probe.method, probe.path, probe.body)
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s as somebody else = %d, want 404", probe.method, probe.path, res.StatusCode)
		}
	}

	// And it is untouched.
	var still pageBody
	h.expect(h.do(http.MethodGet, "/api/pages/"+page.ID, nil), http.StatusOK, &still)
	if still.EditionSize == 11 {
		t.Error("somebody else changed this page")
	}
}

// The reader asks for a page by its address, and for the main page by asking for nothing.
func TestTheEditionEndpointAnswersForOnePage(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "alice")

	var page pageBody
	h.expect(h.do(http.MethodPost, "/api/pages",
		map[string]any{"name": "Finances", "slug": "finances"}), http.StatusCreated, &page)
	h.expect(h.do(http.MethodPatch, "/api/pages/"+page.ID,
		map[string]any{"edition_size": 15}), http.StatusOK, nil)

	// Neither has been composed, so both answer with the empty shape rather than a 404 — the
	// state the reader renders on a new account.
	var main, finances editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &main)
	h.expect(h.do(http.MethodGet, "/api/edition?page=finances", nil), http.StatusOK, &finances)

	if main.Size == finances.Size {
		t.Errorf("both pages report size %d; the parameter was not read", main.Size)
	}
	if finances.Size != 15 {
		t.Errorf("finances reports size %d, want 15", finances.Size)
	}

	if res := h.do(http.MethodGet, "/api/edition?page=nope", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("GET a page that does not exist = %d, want 404", res.StatusCode)
	}
}
