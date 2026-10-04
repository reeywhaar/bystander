package edition

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"bystander/internal/store"
)

// saveSome saves the articles at these positions in the feed, newest first, and returns the
// page of saved articles the first save made.
func (in *instance) saveSome(t *testing.T, at ...int) (*store.Page, []string) {
	t.Helper()
	ctx := context.Background()

	queues, err := in.store.Queues(ctx, in.pageID(), in.principal.ID, []string{in.feed.ID}, 100, nil)
	if err != nil {
		t.Fatalf("Queues(): %v", err)
	}
	var titles []string
	for _, i := range at {
		item := queues[in.feed.ID].Fresh[i]
		if err := in.store.SaveArticle(ctx, in.principal.ID, item.ID); err != nil {
			t.Fatalf("SaveArticle(): %v", err)
		}
		titles = append(titles, item.Title)
	}

	pages, err := in.store.Pages(ctx, in.principal.ID)
	if err != nil {
		t.Fatalf("Pages(): %v", err)
	}
	for _, page := range pages {
		if page.IsSaved {
			return page, titles
		}
	}
	t.Fatal("no page of saved articles after saving")
	return nil, nil
}

// The saved page is composed from what was saved and nothing else, though the same person
// follows a feed with plenty more in it — and in a different order each time, because each
// article is a source of its own rather than one queue read front to back.
func TestTheSavedPageDrawsOnlyWhatWasSaved(t *testing.T) {
	in := newInstance(t, 20)
	page, saved := in.saveSome(t, 2, 5, 7, 11, 13)
	slices.Sort(saved)

	openers := map[string]bool{}
	for range 8 {
		if _, err := in.gen.Regenerate(context.Background(), page.ID, in.store.Now()); err != nil {
			t.Fatalf("Regenerate(): %v", err)
		}
		got := titlesOf(t, in.store, page.ID)
		openers[got[0]] = true
		slices.Sort(got)
		if !slices.Equal(got, saved) {
			t.Fatalf("page holds %v, want exactly what was saved: %v", got, saved)
		}
	}
	if len(openers) < 2 {
		t.Errorf("eight compositions all opened on %v; the saved articles are being read in order", openers)
	}
}

func TestASavedPageWithNothingOnItSaysWhy(t *testing.T) {
	in := newInstance(t, 3)
	page, _ := in.saveSome(t, 0)

	queues, err := in.store.SavedQueues(context.Background(), page.ID, in.principal.ID)
	if err != nil {
		t.Fatalf("SavedQueues(): %v", err)
	}
	for id := range queues {
		if err := in.store.UnsaveArticle(context.Background(), in.principal.ID, id); err != nil {
			t.Fatalf("UnsaveArticle(): %v", err)
		}
	}

	_, err = in.gen.Regenerate(context.Background(), page.ID, in.store.Now())
	if !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "saved") {
		t.Errorf("Regenerate() = %v, want not found, saying nothing has been saved", err)
	}
}
