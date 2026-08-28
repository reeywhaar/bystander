// Package config models bystander's configuration, which is entirely environment-driven.
//
// There is no config file and does not need to be one: four variables, three of which
// have defaults. A file would have to be mounted, kept in sync with the compose stanza
// that mounts it, and parsed — all to express what `docker run -e` already expresses.
//
// What this program *is* — its name, its version, the address it listens on — lives in
// internal/app instead. The line is whether an operator can change it without rebuilding.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Environment variables read by this package.
const (
	// PublicURLEnv is the origin operators open in a browser, e.g. https://read.example.com.
	//
	// It has to be told and cannot be inferred. Host and X-Forwarded-Host are both
	// client-supplied, and an invitation link built from a header a stranger controls is
	// an invitation link a stranger controls.
	PublicURLEnv = "BYSTANDER_PUBLIC_URL"

	// DataDirEnv is where main.db and derived.db live.
	DataDirEnv = "BYSTANDER_DATA_DIR"

	// LogLevelEnv sets the slog level: debug, info, warn or error.
	LogLevelEnv = "BYSTANDER_LOG_LEVEL"

	// BackupDerivedEnv includes derived.db in the archive.
	//
	// Off by default, which is the two-database split showing up in the backup policy:
	// main.db cannot be recovered from anywhere and derived.db mostly can. Mostly, because
	// read_articles lives there — an instance restored without it offers back every article
	// its owner has already read. Worth switching on for most people; not worth deciding for
	// them.
	BackupDerivedEnv = "BYSTANDER_BACKUP_DERIVED"

	// WebDirEnv is where the built frontend lives.
	//
	// Set in the image, and rarely anywhere else. It exists because the bundle is a
	// directory of files beside the binary rather than something compiled into it — see
	// DefaultWebDir for why, and internal/api/spa.go for what is done with it.
	WebDirEnv = "BYSTANDER_WEB_DIR"
)

// Defaults for everything that has one.
const (
	DefaultDataDir = "/data"

	// DefaultWebDir is where the image puts the bundle.
	//
	// A directory read at startup rather than an //go:embed of web/dist, which is what this
	// used to be. Embedding made the bundle an input to the Go compile, so every edit to a
	// stylesheet invalidated the build layer and Docker recompiled and relinked twenty-odd
	// megabytes of binary to serve a file it had already built.
	//
	// Nothing is given up by that. api.NewSPA walks whatever it is handed once, at startup,
	// and copies every file into a map — so the bundle was only ever in memory because of
	// what NewSPA does with it, not because of where it came from. Embedded or read off the
	// disk, the same bytes end up in the same map and the file system is never touched again.
	//
	// A checkout is the other case this has to serve: `go run .` from the repository root
	// finds web/dist, and a tree that has never run `npm run build` finds nothing and gets
	// the placeholder, exactly as an empty embed did.
	DefaultWebDir = "/srv/web"
)

// Config is everything the process was told at startup.
type Config struct {
	// PublicURL is normalised: scheme and host lowercased, no trailing slash, no query
	// or fragment. A path is kept, so bystander can live under a prefix.
	PublicURL *url.URL

	DataDir  string
	LogLevel slog.Level

	// WebDir is the built frontend, read once at startup.
	WebDir string

	// BackupDerived is whether the archive carries derived.db as well as main.db.
	BackupDerived bool

	// Secure is whether the session cookie carries the Secure attribute, which is
	// PublicURL being https and nothing else. Derived here rather than re-decided at
	// each Set-Cookie, so there is one answer.
	Secure bool
}

// Load reads the environment. The returned error is written for somebody looking at a
// container that refused to start.
func Load() (*Config, error) {
	raw := strings.TrimSpace(os.Getenv(PublicURLEnv))
	if raw == "" {
		return nil, fmt.Errorf("%s is required: set it to the address you open in a browser, e.g. https://read.example.com", PublicURLEnv)
	}
	public, err := parsePublicURL(raw)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		PublicURL: public,
		DataDir:   DefaultDataDir,
		WebDir:    DefaultWebDir,
		LogLevel:  slog.LevelInfo,
		Secure:    public.Scheme == "https",
	}

	if dir := strings.TrimSpace(os.Getenv(DataDirEnv)); dir != "" {
		cfg.DataDir = dir
	}

	if dir := strings.TrimSpace(os.Getenv(WebDirEnv)); dir != "" {
		cfg.WebDir = dir
	}

	if v := strings.TrimSpace(os.Getenv(LogLevelEnv)); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return nil, fmt.Errorf("%s: %q is not a level (debug, info, warn, error)", LogLevelEnv, v)
		}
	}

	if v := strings.TrimSpace(os.Getenv(BackupDerivedEnv)); v != "" {
		on, err := parseBool(BackupDerivedEnv, v)
		if err != nil {
			return nil, err
		}
		cfg.BackupDerived = on
	}

	return cfg, nil
}

func parseBool(name, v string) (bool, error) {
	on, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %q is not true or false", name, v)
	}
	return on, nil
}

func parsePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %q is not a URL: %w", PublicURLEnv, raw, err)
	}
	switch u.Scheme {
	case "http", "https":
	case "":
		return nil, fmt.Errorf("%s: %q has no scheme; write it in full, e.g. https://%s", PublicURLEnv, raw, raw)
	default:
		return nil, fmt.Errorf("%s: %q is not http or https", PublicURLEnv, raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%s: %q names no host", PublicURLEnv, raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	// Neither belongs in a base that only ever has paths appended to it, and carrying
	// them would produce links with the query in the middle.
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// Link builds an absolute URL for a path within this service, for the links that leave
// the process: an invitation mailed, printed or pasted into a chat.
func (c *Config) Link(path string) string {
	return c.PublicURL.String() + "/" + strings.TrimPrefix(path, "/")
}

// InsecurePublicURL reports whether the public URL is plain http, which means the session
// cookie ships without Secure. Legitimate behind a terminating proxy on a private
// network, and a real mistake anywhere else, so serve warns rather than refuses.
func (c *Config) InsecurePublicURL() bool { return c.PublicURL.Scheme == "http" }
