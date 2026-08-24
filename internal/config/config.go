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
)

// Defaults for everything that has one.
const (
	DefaultDataDir = "/data"
)

// Config is everything the process was told at startup.
type Config struct {
	// PublicURL is normalised: scheme and host lowercased, no trailing slash, no query
	// or fragment. A path is kept, so bystander can live under a prefix.
	PublicURL *url.URL

	DataDir  string
	LogLevel slog.Level

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
		LogLevel:  slog.LevelInfo,
		Secure:    public.Scheme == "https",
	}

	if dir := strings.TrimSpace(os.Getenv(DataDirEnv)); dir != "" {
		cfg.DataDir = dir
	}

	if v := strings.TrimSpace(os.Getenv(LogLevelEnv)); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return nil, fmt.Errorf("%s: %q is not a level (debug, info, warn, error)", LogLevelEnv, v)
		}
	}

	return cfg, nil
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
