# Deploy

One image, one port, one volume.

```
ghcr.io/reeywhaar/bystander:latest
```

## Environment

| variable | default | meaning |
| --- | --- | --- |
| `BYSTANDER_PUBLIC_URL` | *required* | Origin for generated links, e.g. `https://read.example.com` |
| `BYSTANDER_DATA_DIR` | `/data` | Where `main.db` and `derived.db` live |
| `BYSTANDER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `BYSTANDER_BACKUP_DERIVED` | `false` | Whether the archive carries `derived.db` too |

`BYSTANDER_PUBLIC_URL` **has to be told and cannot be inferred.** `Host` and
`X-Forwarded-Host` are both client-supplied, and an invitation link built from a header a
stranger controls is an invitation link a stranger controls. Startup fails without it
rather than guessing.

It also decides whether the session cookie carries `Secure`, so an `http://` value in
production is a real mistake and is logged as a warning at startup.

There is no config file. Three variables do not need one.

That includes the list of proxies to trust, which most things this shape make you configure.
The account page lists the addresses a session was last used from, and those come from
`X-Forwarded-For` — a header written by whoever sends it. Rather than a fourth variable, a
forwarded header is believed only when the machine that handed us the request is itself on
the loopback or a private network: that is where a reverse proxy in a compose file sits, and
it is not where the internet is. Exposed directly, the header is ignored and the peer's own
address is used, so nobody can write their own address into somebody's session list.

Two consequences worth knowing. If your proxy sits on a **public** address, forwarded
headers are ignored and every session lists the proxy — put it on the same private network
as bystander, which is the ordinary arrangement anyway. And if your proxy does **not** set
`X-Forwarded-For` or `X-Real-IP`, every session lists the proxy's private address, which is
true but useless; nginx wants `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`
and Caddy and Traefik do it by default.

How often a feed is fetched used to be one of them and is not any more: it is worked out per
feed from what that feed publishes — see `feeds.Cadence` — because nobody configuring a reader
knows how often each publisher they follow puts something out.

## Port

`:80`, inside the container, not configurable. Remap it with `-p`. A port number inside a
container is not a thing an operator should have to think about twice.

## Volume

`/data`, holding both SQLite databases and their WAL sidecars.

```
docker run -d --name bystander \
  -e BYSTANDER_PUBLIC_URL=https://read.example.com \
  -v bystander-data:/data \
  -p 8080:80 \
  ghcr.io/reeywhaar/bystander:latest
```

Mount it. Without it every account disappears when the container is replaced, and the way
back in is the first-run invitation link printed to a log that is also gone.

### Backups

`main.db` is the only file worth backing up — accounts, subscriptions, tags. `derived.db`
is items and the current editions, and losing it costs one fetch cycle. That asymmetry is
the reason for the split; see [entities.md](entities.md).

Back it up with `sqlite3 main.db ".backup out.db"` or a filesystem snapshot, not `cp` — a
plain copy of a WAL database while it is being written is a copy of an inconsistent
moment.

## First run

If no admin exists, `serve` creates one and prints an invitation link built from
`BYSTANDER_PUBLIC_URL`:

```
docker logs bystander
```

There is no default password at any point, so there is no credential for somebody to
forget to change. `docker exec bystander bystander invite` reprints a fresh link when the
first one scrolled out of the log.

## Health

```
HEALTHCHECK CMD ["bystander", "healthcheck"]
```

The binary calls itself over loopback, so the image needs no HTTP client and a process
that is running but wedged fails the check. `GET /healthz` returns `{"ok": true}` and the
version.

## The image

Multi-stage:

1. **Bundle** — `node:current-alpine`, pinned to `--platform=$BUILDPLATFORM`. The output is
   architecture-independent, so there is no reason to run npm under QEMU. Lockfile copied
   first so `npm ci` caches until a dependency actually changes. All four HTML entries
   asserted non-empty afterwards — an empty build is otherwise invisible until somebody
   loads the page.
2. **Gzip** — the bundle is compressed before embedding. Smaller binary, and the
   compressed bytes are served from memory rather than recompressed per request. Text
   only; gzipping a `.webp` makes it bigger.
3. **Binary** — `golang:1.27-alpine`, cross-compiled, `CGO_ENABLED=0`, `-trimpath`,
   version stamped through `-ldflags`. Static because `modernc.org/sqlite` is pure Go.
4. **Runtime** — `alpine:latest` plus `ca-certificates`, which is required rather than
   habitual: feeds are fetched over HTTPS.

`COPY` names each path rather than relying on a `.dockerignore`. An allowlist cannot
accidentally admit `web/node_modules`, a local `data/` with real accounts in it, or any of the
untracked working directories a checkout tends to grow.

```dockerfile
ENTRYPOINT ["bystander"]
CMD ["serve"]
```

Split so `docker compose up` runs the daemon while `docker run --rm IMAGE invite` replaces
the command rather than appending to it.

## The backup image

A second image, `backup/Dockerfile`, published as `ghcr.io/reeywhaar/bystander-backup`. It
fetches `GET /backup` on a loop, optionally encrypts with 7-Zip's AES-256, uploads, and prunes.

A separate image rather than a second command in the main one: it has nothing to do with
serving a page, it wants a different restart cadence, and keeping it out means the reader image
gains neither 7-Zip nor a reason to hold an upload credential.

It builds from the repo root so it can see `go.mod`, and skips `go mod download` — `backup/`
imports only the standard library, checked with an empty module cache and `GOPROXY=off` rather
than assumed. 7-Zip comes from the project's own static build pinned by checksum, not `apk add
7zip`: the packaged binary links libstdc++, which is 2.8 MB of an image whose Go binary is a
few, and a tarball from a web server arrives unsigned so the digest is the only thing between a
compromised mirror and a binary that handles the backup password.

The schedule lives in `backup_loop.sh` rather than inside the process, so a stuck run cannot
silently skip the next one — the container restarts instead.

## CI

`.github/workflows/publish.yml`, three jobs.

**test** — `gofmt -l`, `go vet ./...`, `go test ./...`, then `npm ci`, `npm run typecheck`,
`npm test`. The Go tests run with no frontend build present; `web/dist/.gitkeep` is tracked
precisely so they can.

**publish** — needs `test`. Builds both images, the sidecar as a step on the same job so it
reuses the checkout, QEMU, buildx and GHCR login, with a cache scope of its own because the two
share no layers. Publishing `latest` unconditionally means a broken commit
becomes the image everyone pulls, and the tests take under a minute. Builds
`linux/amd64,linux/arm64` to GHCR with the built-in `GITHUB_TOKEN`; no secret to configure.
QEMU is set up only for the final Alpine stage — everything expensive is pinned to
`$BUILDPLATFORM` and cross-compiles.

Then it smoke-tests **the published image**: `/healthz`, and a check that the served page
is not the placeholder. That last one is the only failure that otherwise reaches users
looking exactly like a successful publish.

**notify** — Telegram on either outcome, gated on the secrets existing so a repo that has
not set them up does not get a red run on every push. It says which half broke: a failing
test means the commit is bad, a failing publish means the commit is fine and the build is
not.

`concurrency` with `cancel-in-progress`, so a newer commit wins the race for `latest`
rather than queueing behind an older one that would overwrite it.

## Local

Building the image and running it against a local environment file, and building for amd64 and
piping it to a server over ssh, are each about four lines of bash. They are not in the
repository: both carry a particular deployment's hostnames, paths and keys, and a script that
has to be edited before it can be run is worse than the two commands it replaces.

```
docker build -t bystander:local .
docker run --rm -p 8080:80 -v bystander-data:/data \
  -e BYSTANDER_PUBLIC_URL=http://localhost:8080 bystander:local
```
