# Stack

Every dependency is a thing that can break, change under us, or need explaining to
somebody reading this in a year. Each one below earns its place.

## Backend

| what | version | why |
| --- | --- | --- |
| Go | 1.27 | Routing patterns with methods (`GET /api/feeds/{id}`) removed the last reason to take a router dependency |
| `net/http` | stdlib | See above. No chi, no gorilla, no gin |
| `log/slog` | stdlib | Structured logging without a dependency |
| `github.com/spf13/cobra` | latest | `serve`, `healthcheck`, `invite` — subcommands rather than flags, so `docker exec bystander invite` reads as what it does |
| `modernc.org/sqlite` | v1.57.0 | Pure Go. No cgo means `CGO_ENABLED=0`, a static binary, and an Alpine image with no toolchain in it |
| `github.com/mmcdole/gofeed` | v1.4.2 | RSS, Atom and JSON Feed behind one type. The alternative is three parsers and a format sniffer |
| `golang.org/x/crypto` | latest | bcrypt, cost 12 |
| `golang.org/x/net/html` | latest | Sanitizing feed HTML. A tokenizer over an allowlist, not a regex |

That is the whole list, and it should stay short enough to read.

### Not used, deliberately

- **No ORM, no query builder.** Every query is SQL in `internal/store`. The schema is
  twenty-one tables across the two databases; an abstraction over it would be larger than it.
- **No migration framework.** An append-only list of Go `Migration` values tracked by
  `PRAGMA user_version`, each in its own file and its own transaction. Go rather than `.sql`
  because the migrations that are not just SQL are the ones that matter — moving data between
  the two databases cannot be expressed in a file handed to one connection. See
  [entities.md](entities.md#migrations).
- **No config file.** Three environment variables. See [deploy.md](deploy.md).
- **No CORS middleware.** The browser only ever talks to this origin. Its absence is
  load-bearing — see [api_design.md](api_design.md).

## Frontend

| what | version | why |
| --- | --- | --- |
| React | 19 | — |
| TanStack Query | 5 | Every read is a server read. There is no client state worth a store |
| react-router | 8 | Inside each island, for its own sub-routes |
| Tailwind | 4, via `@tailwindcss/vite` | — |
| TypeScript | 7 | `strict`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax` |
| Vite | 8 | One config, four entries |
| Vitest | 4 | Tests beside sources |
| Prettier | 3 | Formatting is not a discussion |

### Not used, deliberately

- **No state manager.** Query owns server state; `useState` owns the rest.
- **No component library.** A handful of primitives under `src/components/ui`, written here.
  A dozen components is less code than the configuration a library needs to look like this.
- **No masonry library.** The layout is decided server-side and rendered as a CSS grid.
  See [edition.md](edition.md).

## Runtime

Alpine, port 80, `/data` as a volume, published to `ghcr.io/reeywhaar/bystander`.
See [deploy.md](deploy.md).

## Keeping versions honest

Dependabot is not configured and does not need to be at this size. When a version here
moves, move it in this table too — a stack document that lags the lockfile is how
somebody ends up debugging against the wrong changelog.
