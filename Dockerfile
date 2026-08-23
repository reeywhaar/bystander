ARG GO_VERSION=1.27
# node:current-alpine rather than a pinned major: the bundle is plain ES modules and depends
# on no Node API that moves between releases. Pin it (e.g. node:26-alpine) if one ever does.
ARG NODE_IMAGE=node:current-alpine

# ---------------------------------------------------------------------------
# The bundle.
#
# --platform=$BUILDPLATFORM pins this stage to the machine doing the building: the output is
# architecture-independent, so there is no reason to run npm under QEMU.
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
WORKDIR /src/web
# The lockfile alone first, so `npm ci` is cached until a dependency actually changes.
COPY web/package.json web/package-lock.json ./
RUN npm ci

# All four HTML entries. They are four applications with four audiences — the login shell is
# the only document an unauthenticated visitor receives, and the admin bundle is never sent
# to an ordinary account. vite.config.ts names all four, so a missing one is a hard error
# rather than a quietly smaller build.
COPY web/tsconfig.json web/vite.config.ts ./
COPY web/index.html web/login.html web/manage.html web/admin.html ./
COPY web/public ./public
COPY web/src ./src
RUN npm run build

# Fail here, not in production. An empty build is otherwise invisible until somebody loads a
# page and gets the placeholder, or opens an invitation and gets the reader's shell.
RUN for entry in index login manage admin; do \
      test -s "dist/$entry.html" || { echo "dist/$entry.html is missing or empty"; exit 1; }; \
    done

# Gzip the bundle before it is embedded, replacing each original: it takes a few hundred
# kilobytes off the binary, and the compressed bytes are then served from memory rather than
# recompressed per request. internal/api/spa.go registers a "foo.js.gz" under "/foo.js", so
# nothing downstream knows.
#
# Text only, since gzipping a raster image makes it bigger. -9 because this runs once.
RUN find dist -type f \( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.svg' -o -name '*.json' -o -name '*.map' \) \
      -exec gzip -9 {} + && \
    echo "embedded bundle: $(du -sh dist | cut -f1)"

# ---------------------------------------------------------------------------
# The binary.
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Copied path by path rather than `COPY . .` with a .dockerignore: an allowlist cannot
# accidentally admit web/node_modules, a local data/ directory with real accounts in it, or
# the private/ directory.
COPY main.go ./
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist

# CGO_ENABLED=0 because modernc.org/sqlite is pure Go — which is the whole reason the
# runtime stage needs no toolchain and the binary is static.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -buildid= -X bystander/internal/api.Version=${VERSION}" \
      -o /out/bystander .

# ---------------------------------------------------------------------------
# Runtime.
#
# The only stage that runs on the target architecture, and it installs one package — so a
# multi-platform build emulates one short apk step rather than a compile.
# ---------------------------------------------------------------------------
FROM alpine:latest
# Required rather than habitual: feeds are fetched over HTTPS.
RUN apk add --no-cache ca-certificates

COPY --from=build /out/bystander /usr/local/bin/bystander

# main.db and derived.db live here. Mount it, or every account disappears when this
# container is replaced — and the way back in is the first-run invitation link, printed to
# a log that is also gone.
RUN mkdir -p /data
VOLUME /data

# Documentation only. The listen port is not configurable; remap it with -p.
EXPOSE 80

# Runs our own binary rather than shelling out to wget, so the image needs no HTTP client.
# A loopback GET exercises the listener, the router and the handler chain, so a process that
# is running but wedged fails it.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["bystander", "healthcheck"]

# Split so `docker compose up` runs the daemon while `docker run --rm IMAGE invite` replaces
# the command rather than appending to it.
ENTRYPOINT ["bystander"]
CMD ["serve"]
