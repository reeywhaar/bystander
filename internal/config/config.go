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
	"time"
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

	// BackupURLEnv is where a backup agent takes an archive — backio-agent's POST /backup,
	// so on a compose network something like "http://backup:8080/backup".
	//
	// The whole address rather than a host, because the endpoint is the agent's to name and
	// this program should not be the place that knows the path it happens to serve on today.
	//
	// Nothing is backed up unless this is set. There is no default, because a default would
	// be a guess at a hostname on a network this program cannot see, and the failure it
	// produces is a log line every few minutes about somewhere nobody meant to send anything.
	BackupURLEnv = "BYSTANDER_BACKUP_URL"

	// BackupModeEnv is what goes in the archive and what makes one happen — see [BackupMode].
	BackupModeEnv = "BYSTANDER_BACKUP_MODE"

	// WebDirEnv is where the built frontend lives.
	//
	// Set in the image, and rarely anywhere else. It exists because the bundle is a
	// directory of files beside the binary rather than something compiled into it — see
	// DefaultWebDir for why, and internal/api/spa.go for what is done with it.
	WebDirEnv = "BYSTANDER_WEB_DIR"
)

// BackupMode is what goes into an archive and what makes one happen.
//
// Two questions with three useful answers between them, rather than two switches with a
// meaningless fourth combination. "derived only, when main changes" is not a policy anybody
// wants, and a pair of booleans would offer it.
type BackupMode string

const (
	// BackupMain carries main.db alone.
	//
	// The smallest thing worth keeping. main.db is what somebody typed — accounts, feeds,
	// tags, pages, settings — and the one file that cannot be rebuilt from anywhere.
	BackupMain BackupMode = "main"

	// BackupRelaxed carries both, and is the default.
	//
	// derived.db comes along for the ride but never decides that a copy is due. What it holds
	// is mostly rebuildable by one fetch cycle — mostly, because read_articles lives there,
	// and an instance restored without it offers back every article its owner has already
	// read. Cheap to carry, so carried; not worth waking up for, so it does not wake anything.
	BackupRelaxed BackupMode = "relaxed"

	// BackupAll is relaxed with a floor under it: a copy at least every [BackupAllPeriod],
	// whether or not anybody touched a setting.
	//
	// The one mode that sends an archive when nothing a person did has changed. For an
	// operator who wants what was *read* kept closely rather than as of the last time somebody
	// added a feed — that record only ever changes by reading, which main.db never sees.
	BackupAll BackupMode = "all"
)

// How long a copy waits, and how long an instance can go without one.
//
// Constants, not settings, and deliberately. Neither is a number an operator can reason about
// better than this program can: the delay trades "how much of a burst becomes one archive"
// against "how long a change sits uncopied", and the floor is only there to catch reading,
// which is the one thing main.db never sees. Both have one right answer for every instance
// this runs on, and a knob would mostly be a way to set them wrong.
//
// What an operator actually chooses is which of those promises they want, and that is the
// mode.
const (
	// BackupDelay is how long after a change the copy goes out, in every mode.
	//
	// A delay and a throttle at once: nothing here reacts to a write, so somebody adding six
	// feeds in a minute gets one archive holding all six rather than six archives.
	BackupDelay = 5 * time.Minute

	// BackupAllPeriod is how long [BackupAll] will go without sending anything.
	BackupAllPeriod = 30 * time.Minute
)

// Valid reports whether m is a mode this program knows.
func (m BackupMode) Valid() bool {
	return m == BackupMain || m == BackupRelaxed || m == BackupAll
}

// Derived reports whether the archive carries derived.db.
func (m BackupMode) Derived() bool { return m == BackupRelaxed || m == BackupAll }

// Period is how long a mode will go without sending anything, or zero for no floor at all.
//
// Only [BackupAll] has one. Every mode sends when main.db changes; this is the extra promise
// that one of them makes on top.
func (m BackupMode) Period() time.Duration {
	if m == BackupAll {
		return BackupAllPeriod
	}
	return 0
}

// Defaults for everything that has one.
const (
	DefaultDataDir = "/data"

	// DefaultBackupMode carries both databases and copies them when main.db changes.
	//
	// Relaxed rather than main, because derived.db is nearly free to carry and holds the one
	// thing in it that cannot be refetched: what its owner has already read.
	DefaultBackupMode = BackupRelaxed

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

	// BackupURL is where an archive is posted, or empty for no backups at all.
	BackupURL string
	// BackupMode is what goes in the archive, and what makes one happen.
	BackupMode BackupMode

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

	cfg.BackupURL = strings.TrimSpace(os.Getenv(BackupURLEnv))
	cfg.BackupMode = DefaultBackupMode
	if v := strings.TrimSpace(os.Getenv(BackupModeEnv)); v != "" {
		mode := BackupMode(strings.ToLower(v))
		if !mode.Valid() {
			return nil, fmt.Errorf("%s: %q is not one of main, relaxed or all", BackupModeEnv, v)
		}
		cfg.BackupMode = mode
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
