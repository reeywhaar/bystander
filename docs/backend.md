# Backend

Go 1.27. Standard library where it reaches; see [stack.md](stack.md) for the four
dependencies that go beyond it.

## Layout

```
main.go              ten lines
internal/
  app/               what this program is: name, version, project, listen address
  cli/               cobra commands
  config/            the environment, parsed once
  store/             both databases, migrations, every SQL statement
  session/           the logged-in table
  feeds/             fetching, parsing, sanitizing, measuring pictures
  edition/           selection, slot assignment, the scheduler
  jobs/              the background queue: one goroutine per kind, one policy each
  mail/              SMTP, and the one message this sends
  ids/               prefixed, sortable identifiers
  api/               HTTP handlers, middleware, SPA serving
web/
  embed.go           //go:embed all:dist
```

`main.go` does two things and then gets out of the way:

```go
func main() {
	// Every time this prints is UTC, whatever TZ says. The image carries no zone
	// database, so a named TZ would resolve to UTC regardless — pinning it makes that
	// a decision, not a side effect of what the runtime happens to contain.
	time.Local = time.UTC
	os.Exit(cli.Execute())
}
```

### `internal/app` against `internal/config`

`app` is what the binary **is**: its name, the version stamped into it at link time, the
project it comes from, the port it listens on. `config` is what it was **told**: the public
URL, the data directory, the poll interval, the log level.

The line is whether an operator can change it without rebuilding. They can move the port
with `-p`; they cannot make bystander listen on anything but `:80` inside the container, any
more than they can make it a different project.

`app` deliberately does not become a home for every constant in the program. Priorities,
retentions and the session cookie's name are domain rules and belong beside the code that
enforces them — a `constants` package that collects them is a package with no subject, and
the first thing anybody does with one is stop reading it.

The version is a variable rather than a constant for one reason:

```
-ldflags "-X bystander/internal/app.Version=$(git rev-parse --short HEAD)"
```

It used to live in `internal/api`, which meant the Dockerfile reached into the HTTP package
to stamp a build number.

### Dependency direction

```
cli → api → edition → feeds → store
              ↘        ↘       ↗
               session ────────
config is read by cli and passed down; nothing imports it upward.
app imports nothing and is imported freely — that is what a package of four facts is for.
```

Nothing under `internal/` imports `web`. `internal/api` takes an `fs.FS`, which is both
what keeps the direction clean and what lets its tests drive an `fstest.MapFS` instead of
whatever the real bundle happens to contain.

**`web/embed.go` cannot move into `internal/`.** `//go:embed` patterns may not contain
`..`, so no package below `web/` can reach `web/dist`. This is a language rule, not a
preference.

## `internal/store`

The only package that writes SQL. Handlers do not build queries, and `edition` does not
reach past it into the database.

Two handles, `main` and `derived`, never `ATTACH`ed. Every connection is opened with
`journal_mode(WAL) busy_timeout(5000) synchronous(NORMAL) foreign_keys(ON)` and
`SetMaxOpenConns(1)`. The pragmas are **verified after opening** — a `foreign_keys=OFF`
that was silently ignored permits orphaned rows for the lifetime of the connection, and
finding that out at startup is much cheaper than finding it out from the data.

Migrations are Go files under `internal/store/migrations/`, one per change, named
`<timestamp>_<database>_<name>.go` and tracked by `PRAGMA user_version`. Go rather than
`.sql` because a migration gets a handle on the *other* database and can move data across.
Never edit a released one — the hash in `migrations_test.go` fails the build if you do.

Full schema and the migration rules in [entities.md](entities.md).

### The error vocabulary

```go
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
	ErrInvalid  = errors.New("invalid")
)
```

Three sentinels, wrapped with context at the point they are returned, mapped to status
codes in exactly one place in `internal/api`. A handler that switches on a driver error is
a handler that will disagree with another handler eventually.

## `internal/session`

Sessions are persisted in `main.db` rather than in a JSON file beside it, so they take part in
the same transactions as the accounts they belong to — changing a password and ending that
account's sessions is one commit or neither.

Keyed by `sha256(cookie value)`. The hash is what is stored and what is compared, so
nothing readable — a backup, a heap dump, a swapped page — ever contains a replayable
credential, and the lookup is timing-safe without trying to be.

Sliding one-week expiry, refresh throttled to once an hour, sweep every ten minutes. The
throttle is not an optimisation: without it a polling SPA rewrites the row and emits a
`Set-Cookie` on every request for a window measured in days.

Changing a password deletes that principal's sessions. Disabling an account does the same.

## `internal/feeds`

Fetching, parsing and sanitizing, and measuring the pictures that come out of it. One HTTP
client with a timeout and a response size limit.

**Fetching is a job kind, not a poller.** It was a poller — its own goroutine on its own
cadence — and moving it into `internal/jobs` as `feed.fetch` is what makes all background work
one queue speaking one voice: `job started` / `job ended` / `job failed`, with a kind, a label
and a duration. The refill queues feeds whose `next_fetch_at` has come; the policy registers
`MaxAttempts: 1`, because a failed fetch already backs the feed off in the `feeds` table and
retrying the job as well would be two clocks disagreeing about one feed.

- **Conditional GET always.** `etag` and `last_modified` ride back out as `If-None-Match`
  and `If-Modified-Since`.
- **How often is measured, not configured.** `Cadence` takes the median gap between a feed's
  recent articles, floors it at half an hour and caps it at a week — and also at half the span
  between the oldest and newest item the feed is carrying, because a feed holds a fixed number
  of items and waiting longer than that window loses articles permanently. Against nineteen
  real feeds this cut fetches by about four fifths, nearly all of it at the slow end.
- **Backoff on failure**, exponential, capped at six hours, written into
  `feeds.next_fetch_at`. A dead feed is retried occasionally, not every cycle forever.
- **A `User-Agent` on every outbound request**, carrying the build, the project and this
  instance's own address:

  ```
  bystander/a1b2c3d (+https://github.com/reeywhaar/bystander; +https://read.example.com)
  ```

  Not politeness for its own sake. An anonymous or absent User-Agent is what gets a fetcher
  rate-limited or blocked outright, and a publisher who wants it to stop needs somewhere to
  look: the project link says what the software is, the instance link says who to talk to
  about this particular one, and the build says which version misbehaved.
  `TestEveryRequestIdentifiesItself` covers every path that leaves the process, the address
  guessing included — that one asks for several addresses a site never advertised, so it is
  the path that most needs to say who it is.
- **Sanitize at ingest.** A tokenizer over an allowlist — see
  [entities.md](entities.md#items). Every reader of the table gets the safe form by
  construction.
- **Redirects followed to a small limit**; a permanent redirect updates `canonical_url`
  unless that would collide with an existing feed.
- **No fetch is triggered by a request** except `POST /api/feeds`, which is rate limited.

Fetching must never be the reason a read is slow. It writes to `derived.db`; the reader reads
from it; WAL means neither blocks the other.

**Measuring a picture** lives here too, as the `image.measure` kind. `image.DecodeConfig` reads
a header and stops, so with a `Range` request and a 64KB ceiling a 3.2MB picture is measured by
reading 16KB of it. A failure is postponed with a reason rather than settled — see
[entities.md](entities.md#measuring-a-picture), which is where the reasons and their waits
are.

## `internal/edition`

Selection and slot assignment, per [edition.md](edition.md), plus the scheduler that
decides when.

Pure where it can be: the sampler takes queues, weights and a seed, and returns ranks. It
does not open a transaction, read a clock or touch the store, which is what makes it
testable against a fixed seed rather than against a database.

The scheduler ticks every minute and composes for whichever *page* is due — per page rather
than per person, since a person has as many as they like and each carries its own cadence. One
composition at a time.

Slot assignment reads the picture's measured shape, which is the one input that can arrive
after the article did; see [edition.md](edition.md#which-cards-are-widened-and-how-wide).

## `internal/api`

Routing is `net/http` with method patterns:

```go
mux.HandleFunc("GET /healthz", s.healthz)
mux.HandleFunc("POST /api/login", s.login)
mux.Handle("GET /api/edition", s.requireSession(s.edition))
mux.Handle("POST /api/admin/invites", s.requireAdmin(s.createInvite))
mux.Handle("/", s.spa)
```

`requireSession` and `requireAdmin` are the only two authorisation decisions in the
program, and they are made at registration. A handler cannot forget to check, because a
handler that is registered without one is visibly registered without one.

`/api/` has a catch-all returning a JSON `404`, so a mistyped API path never falls through
to the SPA and comes back as an HTML document a `fetch` cannot parse.

### Serving the SPA

The rules it follows:

- Every file read into memory once at construction, with its ETag and content type
  precomputed. A bundle is a few hundred kilobytes; this sidesteps every
  `http.FileServerFS` quirk that would otherwise need working around.
- Content types come from an explicit table, not `mime.TypeByExtension` — that reads
  `/etc/mime.types`, which a scratch container may not have.
- The build gzips the bundle before embedding. A `foo.js.gz` registers under `/foo.js`, so
  nothing downstream knows; `Vary: Accept-Encoding` is set whenever a file has a
  compressed form, and the ETag is keyed on the *uncompressed* bytes so both
  representations share one validator.
- `/assets/*` is content-hashed by Vite and served `immutable` for a year. Shells are
  `no-cache`, never `no-store`.
- A request that does not look like navigation gets a `404`, not the shell. A missing
  `/app.js` served as HTML presents as a MIME-type error with no hint that the file simply
  is not there.
- Which shell a navigation gets is decided in one function against one prefix table. See
  [frontend.md](frontend.md#islands).
- A missing `dist/index.html` is not fatal: the placeholder page explains itself, and the
  API is still useful.

## Testing

- Tests beside sources. `go test ./...` must pass with **no frontend build present** —
  `web/dist/.gitkeep` is tracked so the embed resolves.
- The store's tests run against a temporary file, not `:memory:`. WAL behaves differently
  in memory, and WAL is the thing being relied on.
- Constructors take `now func() time.Time` so expiry is driven, not slept through.
- The sampler is tested against fixed seeds: same seed, same page.
- One test asserts no `Access-Control-Allow-Origin` is ever emitted, because that absence
  is a security property — see [api_design.md](api_design.md).

## Logging

`log/slog`, level from `BYSTANDER_LOG_LEVEL`.

Log what an operator needs and nothing else: a feed that started failing, a feed that
recovered, an edition generated with how many items from how many feeds, a login refused.
Not every fetch, not every request, and never a token, a cookie value, or a password —
hashed or otherwise.
