package store

import (
	"context"
	"strings"
	"testing"
)

func principal(t *testing.T, s *Store) *Principal {
	t.Helper()
	p, err := s.CreatePrincipal(context.Background(), "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	return p
}

func TestTagPathIsRootFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)

	news, err := s.CreateTag(ctx, p.ID, "News", "", DefaultPriority)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}
	world, err := s.CreateTag(ctx, p.ID, "World", news.ID, 90)
	if err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}

	path, err := s.TagPath(ctx, p.ID, world.ID)
	if err != nil {
		t.Fatalf("TagPath(): %v", err)
	}
	if strings.Join(path, "/") != "News/World" {
		t.Errorf("TagPath() = %v, want [News World]", path)
	}
}

func TestTagByPath(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)

	news, _ := s.CreateTag(ctx, p.ID, "News", "", DefaultPriority)
	world, _ := s.CreateTag(ctx, p.ID, "World", news.ID, DefaultPriority)

	// Names are matched the way somebody types them, not the way they stored them.
	for _, path := range [][]string{{"News", "World"}, {"news", "world"}, {"NEWS", "World"}} {
		got, err := s.TagByPath(ctx, p.ID, path)
		if err != nil {
			t.Fatalf("TagByPath(%v): %v", path, err)
		}
		if got == nil || got.ID != world.ID {
			t.Errorf("TagByPath(%v) = %v, want World", path, got)
		}
	}

	// "You do not have this one" is an answer, not a failure — it is what an import
	// preview is asking for.
	for _, missing := range [][]string{{"Sport"}, {"News", "Local"}, {"World"}} {
		got, err := s.TagByPath(ctx, p.ID, missing)
		if err != nil {
			t.Errorf("TagByPath(%v) errored: %v", missing, err)
		}
		if got != nil {
			t.Errorf("TagByPath(%v) = %v, want nothing", missing, got)
		}
	}
}

func TestEnsureTagPathCreatesWhatIsMissing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)

	leaf, err := s.EnsureTagPath(ctx, p.ID, []string{"News", "World", "Europe"})
	if err != nil {
		t.Fatalf("EnsureTagPath(): %v", err)
	}
	if leaf.Name != "Europe" {
		t.Fatalf("leaf = %q, want Europe", leaf.Name)
	}

	// The whole path, not just the leaf: half a path is not a place.
	tags, err := s.ListTags(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListTags(): %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("%d tags created, want 3: %v", len(tags), tags)
	}
	path, err := s.TagPath(ctx, p.ID, leaf.ID)
	if err != nil {
		t.Fatalf("TagPath(): %v", err)
	}
	if strings.Join(path, "/") != "News/World/Europe" {
		t.Errorf("path = %v", path)
	}
}

// Importing the same list twice must not double somebody's taxonomy.
func TestEnsureTagPathReusesWhatIsThere(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)

	news, _ := s.CreateTag(ctx, p.ID, "News", "", 90)

	leaf, err := s.EnsureTagPath(ctx, p.ID, []string{"news", "World"})
	if err != nil {
		t.Fatalf("EnsureTagPath(): %v", err)
	}
	if leaf.ParentID != news.ID {
		t.Errorf("World was nested under %q, want the existing News (%q)", leaf.ParentID, news.ID)
	}

	again, err := s.EnsureTagPath(ctx, p.ID, []string{"News", "World"})
	if err != nil {
		t.Fatalf("second EnsureTagPath(): %v", err)
	}
	if again.ID != leaf.ID {
		t.Errorf("a second import made a second tag: %q then %q", leaf.ID, again.ID)
	}

	tags, _ := s.ListTags(ctx, p.ID)
	if len(tags) != 2 {
		t.Errorf("%d tags after importing twice, want 2", len(tags))
	}
	// The priority somebody set on their own tag is theirs, not the file's.
	kept, _ := s.TagByID(ctx, p.ID, news.ID)
	if kept.Priority != 90 {
		t.Errorf("News came back at priority %d, want the 90 it was set to", kept.Priority)
	}
}

// One person's taxonomy is their own.
func TestTagPathsAreScoped(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	alice := principal(t, s)
	bob, err := s.CreatePrincipal(ctx, "bob", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if _, err := s.CreateTag(ctx, alice.ID, "Secret", "", DefaultPriority); err != nil {
		t.Fatalf("CreateTag(): %v", err)
	}

	got, err := s.TagByPath(ctx, bob.ID, []string{"Secret"})
	if err != nil {
		t.Fatalf("TagByPath(): %v", err)
	}
	if got != nil {
		t.Error("bob can resolve alice's tag")
	}
}

// A feed's tags have to read in the same sequence wherever they appear. Unordered, the row
// under a feed said "Tech · Design" while the dialog showed the same two as "Design, Tech".
func TestAFeedsTagsComeBackInOneOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := principal(t, s)

	// Created in an order that is not their sorted order.
	var ids []string
	for _, name := range []string{"Tech", "Art", "Design", "Comics"} {
		tag, err := s.CreateTag(ctx, p.ID, name, "", DefaultPriority)
		if err != nil {
			t.Fatalf("CreateTag(%q): %v", name, err)
		}
		ids = append(ids, tag.ID)
	}

	feed, err := s.UpsertFeed(ctx, "https://example.com/rss", "Example", "")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	// Attached in yet another order.
	sub, err := s.Subscribe(ctx, p.ID, feed.ID, DefaultPriority, []string{ids[0], ids[2]})
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}

	names := func(tagIDs []string) []string {
		out := make([]string, 0, len(tagIDs))
		for _, id := range tagIDs {
			tag, err := s.TagByID(ctx, p.ID, id)
			if err != nil {
				t.Fatalf("TagByID(): %v", err)
			}
			out = append(out, tag.Name)
		}
		return out
	}

	one, err := s.SubscriptionByID(ctx, p.ID, sub.ID)
	if err != nil {
		t.Fatalf("SubscriptionByID(): %v", err)
	}
	if got := strings.Join(names(one.TagIDs), ", "); got != "Design, Tech" {
		t.Errorf("one subscription's tags = %q, want them sorted", got)
	}

	all, err := s.ListSubscriptions(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListSubscriptions(): %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("%d subscriptions", len(all))
	}
	if got := strings.Join(names(all[0].TagIDs), ", "); got != "Design, Tech" {
		t.Errorf("the listing's tags = %q, want the same order", got)
	}
}
