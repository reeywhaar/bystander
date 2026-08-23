package store

import (
	"context"
	"testing"
	"time"
)

// Retention used to be a flat thirty days, which quietly made the longer windows a lie: a
// page set to show a year of articles had nothing older than a month to show.
func TestRetentionFollowsTheLongestWindow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	alice, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	bob, err := s.CreatePrincipal(ctx, "bob", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}

	// Everybody on the default week: the floor decides.
	got, err := s.EffectiveItemRetention(ctx)
	if err != nil {
		t.Fatalf("EffectiveItemRetention(): %v", err)
	}
	if got != MinItemRetention {
		t.Errorf("retention = %s with everybody on a week, want the floor of %s", got, MinItemRetention)
	}

	// One person wants a year, so a year is kept — for everybody, because the articles
	// are shared.
	year := 365 * 24 * time.Hour
	if err := s.UpdateSettings(ctx, bob.ID, SettingsPatch{ArticleWindow: &year}); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
	if got, _ = s.EffectiveItemRetention(ctx); got != year {
		t.Errorf("retention = %s with somebody on a year, want %s", got, year)
	}

	// "No limit" takes the ceiling rather than forever: unbounded growth is not a setting
	// anybody meant to choose.
	none := time.Duration(0)
	if err := s.UpdateSettings(ctx, alice.ID, SettingsPatch{ArticleWindow: &none}); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
	if got, _ = s.EffectiveItemRetention(ctx); got != MaxItemRetention {
		t.Errorf("retention = %s with somebody on no limit, want the ceiling of %s", got, MaxItemRetention)
	}
}

func TestTheWindowIsValidated(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}

	settings, err := s.Settings(ctx, p.ID)
	if err != nil {
		t.Fatalf("Settings(): %v", err)
	}
	if settings.ArticleWindow != DefaultArticleWindow {
		t.Errorf("a new account's window is %s, want %s", settings.ArticleWindow, DefaultArticleWindow)
	}

	odd := 3 * time.Hour
	if err := s.UpdateSettings(ctx, p.ID, SettingsPatch{ArticleWindow: &odd}); err == nil {
		t.Error("UpdateSettings accepted a window that is not on the list")
	}

	for _, valid := range ArticleWindows {
		window := valid
		if err := s.UpdateSettings(ctx, p.ID, SettingsPatch{ArticleWindow: &window}); err != nil {
			t.Errorf("UpdateSettings(%s): %v", window, err)
		}
	}
}
