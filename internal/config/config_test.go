package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoadRequiresPublicURL(t *testing.T) {
	t.Setenv(PublicURLEnv, "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without a public URL")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv(PublicURLEnv, "https://read.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, DefaultDataDir)
	}
	if cfg.FetchInterval != DefaultFetchInterval {
		t.Errorf("FetchInterval = %s, want %s", cfg.FetchInterval, DefaultFetchInterval)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %s, want info", cfg.LogLevel)
	}
	if !cfg.Secure {
		t.Error("Secure = false for an https public URL")
	}
}

func TestPublicURLNormalisation(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://read.example.com/", "https://read.example.com"},
		{"HTTPS://Read.Example.COM", "https://read.example.com"},
		{"https://read.example.com/reader/", "https://read.example.com/reader"},
		{"https://read.example.com/?a=b#c", "https://read.example.com"},
	} {
		t.Setenv(PublicURLEnv, tc.in)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(%q): %v", tc.in, err)
		}
		if got := cfg.PublicURL.String(); got != tc.want {
			t.Errorf("Load(%q).PublicURL = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPublicURLRejects(t *testing.T) {
	for _, bad := range []string{"read.example.com", "ftp://read.example.com", "https://", "::"} {
		t.Setenv(PublicURLEnv, bad)
		if _, err := Load(); err == nil {
			t.Errorf("Load(%q) succeeded, want an error", bad)
		}
	}
}

func TestLink(t *testing.T) {
	t.Setenv(PublicURLEnv, "https://read.example.com/reader")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	// With and without the leading slash, because callers write it both ways.
	const want = "https://read.example.com/reader/invite/i_abc"
	if got := cfg.Link("/invite/i_abc"); got != want {
		t.Errorf("Link(%q) = %q, want %q", "/invite/i_abc", got, want)
	}
	if got := cfg.Link("invite/i_abc"); got != want {
		t.Errorf("Link(%q) = %q, want %q", "invite/i_abc", got, want)
	}
}

func TestFetchIntervalFloor(t *testing.T) {
	t.Setenv(PublicURLEnv, "https://read.example.com")

	t.Setenv(FetchIntervalEnv, "10s")
	if _, err := Load(); err == nil {
		t.Error("Load() accepted a ten-second fetch interval")
	}

	t.Setenv(FetchIntervalEnv, "2h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.FetchInterval != 2*time.Hour {
		t.Errorf("FetchInterval = %s, want 2h", cfg.FetchInterval)
	}
}
