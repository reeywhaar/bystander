package ids

import (
	"testing"
	"time"
)

func TestIdsSortChronologically(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	earlier := newAt(Article, base)
	later := newAt(Article, base.Add(time.Millisecond))

	if !(earlier < later) {
		t.Fatalf("ids do not sort by time: %q >= %q", earlier, later)
	}
}

func TestNewIsUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for range 1000 {
		id := New(Feed)
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestValid(t *testing.T) {
	id := New(Principal)
	if !Valid(Principal, id) {
		t.Errorf("Valid(%q) = false, want true", id)
	}
	// The prefix is part of the shape: a feed id is not a principal id.
	if Valid(Feed, id) {
		t.Errorf("Valid(Feed, %q) = true, want false", id)
	}
	for _, bad := range []string{"", "p_", "p_short", id + "X", "p_IIIIIIIIIIIIIIIIIIIIIIIIII"} {
		if Valid(Principal, bad) {
			t.Errorf("Valid(%q) = true, want false", bad)
		}
	}
}
