#!/usr/bin/env bash
#
# Regenerate the README screenshots.
#
#   docs/screenshots/capture.sh
#
# Builds the frontend and the binary, starts eight stand-in publishers and a reader against
# them, subscribes to all eight through the reader's own API, composes a page, drives
# headless Chromium over the DevTools protocol, and overwrites the PNGs next to this script.
# Everything it starts is stopped again on the way out, including on failure.
#
# Requires: go, node, and Chromium or Chrome. No Docker, and no npm packages beyond the
# frontend's own — Chromium has a headless mode and Node has a WebSocket client, so there is
# nothing here for Playwright to do.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
work="$(mktemp -d)"

# Wide enough for the front page's four columns: the grid drops to three under 1100 and two
# under 820, and a set of screenshots in which the front page is two columns wide sells the
# whole idea short — the layout *is* the product.
WIDTH="${WIDTH:-1240}"
# A window, not the whole page — enough of one to show the lead, both features and the top
# of the standard row, which between them is every slot the grid has. See the comment on the
# front-page shot in shoot.mjs.
HEIGHT="${HEIGHT:-1500}"
# The management pages are `max-w-3xl`, so this is that plus a margin.
NARROW="${NARROW:-880}"
# The theme the committed PNGs are in, so a plain run reproduces them rather than replacing
# the set with the other one.
THEME="${THEME:-light}"
# Where the PNGs land. Overridable so that a look at the other theme does not have to
# overwrite the committed set to be worth taking.
OUT="${OUT:-$here}"

# The listen port is not configurable — it is :80 inside a container and the operator remaps
# it — so this script has to be able to bind :80. Fine on macOS; on Linux it wants either
# root or `sudo setcap cap_net_bind_service=+ep` on the built binary.
BASE="http://localhost"

chromium=""
for candidate in \
  "/Applications/Chromium.app/Contents/MacOS/Chromium" \
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  "$(command -v chromium || true)" \
  "$(command -v chromium-browser || true)" \
  "$(command -v google-chrome || true)"; do
  if [ -n "$candidate" ] && [ -x "$candidate" ]; then chromium="$candidate"; break; fi
done
if [ -z "$chromium" ]; then
  echo "no Chromium or Chrome found; put one on PATH" >&2
  exit 1
fi

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  # Chromium writes its profile out as it exits, so removing the directory immediately races
  # it and leaves "Directory not empty" noise on the way out.
  sleep 1
  rm -rf "$work" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> building the frontend"
(cd "$root/web" && npm ci && npm run build) > "$work/web.log" 2>&1 \
  || { cat "$work/web.log" >&2; exit 1; }

echo "==> building the binary"
(cd "$root" && go build -o "$work/bystander" .)

echo "==> starting the stand-in publishers"
node "$here/stub.mjs" > "$work/stub.log" 2>&1 &
pids+=($!)

# Verify they are *ours*. A stub left running from an earlier attempt keeps the port, the new
# one dies with EADDRINUSE in the background where nothing notices, and the capture then
# quietly succeeds against stale content — which is exactly how a set of screenshots ends up
# showing something the code no longer does.
ready=""
for _ in $(seq 40); do
  if curl -fsS "http://127.0.0.1:8811/f/meridian.xml" 2>/dev/null | grep -q "The Meridian"; then
    ready=1
    break
  fi
  sleep 0.25
done
if [ -z "$ready" ]; then
  echo "the stand-in publishers never answered on :8811 — is something else on that port?" >&2
  cat "$work/stub.log" >&2
  exit 1
fi

export BYSTANDER_PUBLIC_URL="$BASE"
export BYSTANDER_DATA_DIR="$work/data"

# Minted before `serve` starts, deliberately. There is no default account at any point: the
# only way in is an invitation, and bootstrap mints one on an empty database — so doing it
# here means there is exactly one live invitation rather than two.
echo "==> minting an invitation"
invite=$("$work/bystander" invite | head -1)
token="${invite##*/}"
[ -n "$token" ] || { echo "could not mint an invitation"; exit 1; }

echo "==> starting the reader"
"$work/bystander" serve > "$work/serve.log" 2>&1 &
pids+=($!)

for _ in $(seq 40); do
  curl -fsS "$BASE/healthz" > /dev/null 2>&1 && break
  sleep 0.25
done
curl -fsS "$BASE/healthz" > /dev/null || {
  echo "the reader never came up — is something else on :80?" >&2
  cat "$work/serve.log" >&2
  exit 1
}

# Accepting an invitation signs the new account in, so this one request is also the login.
# Two statements rather than one substitution: `$(curl -w '%{http_code}' && awk ...)` would
# capture curl's "204" alongside the cookie, and "204\n<value>" is not a session.
curl -fsS -c "$work/ck" -X POST "$BASE/api/invites/$token/accept" \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"demopassword123"}' -o /dev/null
cookie=$(awk '/bystander_auth/ {print $7}' "$work/ck")
[ -n "$cookie" ] || { echo "could not accept the invitation"; cat "$work/serve.log"; exit 1; }

api() { curl -fsS -b "$work/ck" -H 'Content-Type: application/json' "$@"; }

# Read one value out of a JSON document on stdin, as a JavaScript expression over `d`.
#
# `sed` cannot do this: the API pretty-prints nothing but the shapes are nested, and a
# pattern spanning two fields is a pattern that stops matching the first time a field order
# changes. node is already a hard requirement here.
jget() {
  node -e '
    let raw = "";
    process.stdin.on("data", (chunk) => (raw += chunk)).on("end", () => {
      const expr = process.argv[1];
      let value;
      try {
        value = new Function("d", "return " + expr)(JSON.parse(raw));
      } catch (err) {
        console.error(expr + ": " + err.message);
        process.exit(1);
      }
      if (value === undefined || value === null) {
        console.error(expr + ": no match");
        process.exit(1);
      }
      process.stdout.write(String(value));
    });
  ' "$1"
}

# section|priority|paper:priority ...
#
# One line per section of the paper, and the feeds filed under it. Priorities are a spread
# rather than all fifty, because a screenshot of every slider at the default says nothing
# about what the sliders are for.
#
# The Wire Desk is at 35 with forty-two articles in it, which is the case the sampler exists
# for: a draw picks a feed before it picks an article, so volume buys nothing.
#
# A tag and its feeds go in together rather than through a name-to-id map, because bash 3.2
# — which is what /bin/bash is on macOS — has no associative arrays.
echo "==> filing the sections and subscribing"
while IFS='|' read -r section priority papers; do
  [ -n "$section" ] || continue
  tag_id=$(api -X POST "$BASE/api/tags" \
    -d "{\"name\":\"$section\",\"priority\":$priority}" | jget "d.id")

  for paper in $papers; do
    slug="${paper%%:*}"
    # No `|| true`. A feed that fails to subscribe is the difference between a front page
    # and an empty state, and it is better to hear about that here than to read it in the
    # README.
    api -X POST "$BASE/api/feeds" \
      -d "{\"url\":\"http://127.0.0.1:8811/f/$slug.xml\",\"priority\":${paper##*:},\"tag_ids\":[\"$tag_id\"]}" \
      -o /dev/null
  done
  printf '  %-12s %s\n' "$section" "$papers"
done <<'SECTIONS'
World|60|meridian:75 wiredesk:35
Business|45|ledger:55
Technology|70|copperwire:80
Science|55|fieldnotes:60
Culture|60|undercurrent:65 slowcraft:50
Local|40|harbour:45
SECTIONS

# The poller's first cycle is a minute after startup, and these feeds were added after that,
# so without this wait every row on the feeds page says "not fetched yet" underneath a feed
# whose articles are already on the front page. Adding a feed saves the articles that
# discovery already parsed — which is why there is a page at all — but it is the poller that
# records a successful fetch.
echo "==> waiting for the first poll"
total=$(api "$BASE/api/feeds" | jget "d.length")
fetched=0
for _ in $(seq 60); do
  fetched=$(api "$BASE/api/feeds" | jget "d.filter(f => f.last_success_at).length")
  [ "$fetched" = "$total" ] && break
  sleep 2
done
[ "$fetched" = "$total" ] || {
  echo "only $fetched of $total feeds were ever fetched" >&2
  cat "$work/serve.log" >&2
  exit 1
}

# Twenty-eight rather than the default: a lead, two features and the rest, which is the
# shape the grid was designed around. Eight per cent of the page is features, so anything
# under twenty-five gets exactly one and the second row never appears.
echo "==> setting the page size"
api -X PATCH "$BASE/api/settings" -d '{"edition_size":28}' -o /dev/null

# Re-rolled until the first article is an actual lead, which is a real thing to insist on
# rather than a cheat. An article with no picture and no standfirst is laid out as a brief
# whatever its rank — including at rank 0 — so about one page in four opens with a one-line
# item in the corner. That is honest behaviour and a poor advertisement for a layout whose
# whole argument is the lead. `regenerate` is a re-roll by design: unread articles go back
# into the pool first, so this spends nothing and is what the reader's own "Make a different
# page" button does.
echo "==> composing a page"
for attempt in $(seq 12); do
  api -X POST "$BASE/api/edition/regenerate" -o /dev/null
  edition=$(api "$BASE/api/edition")
  slot=$(printf '%s' "$edition" | jget "d.items[0].slot")
  # The opener's width is drawn from the three that are wider than a column, so any of them
  # is a real page. A brief is not: it means the first article had neither a picture nor a
  # summary, which makes a poor first impression of a layout the README is there to show.
  case "$slot" in
  lead | wide | feature) break ;;
  esac
  echo "  re-rolling: the page opened with a $slot (attempt $attempt)"
done
case "$slot" in
lead | wide | feature) ;;
*) echo "  giving up: twelve pages in a row opened with a $slot" >&2 ;;
esac

# Three read articles, so the front page shows what "read" looks like — greyed and
# desaturated, in place — and so Recently read has something in it. Not the first three: a
# greyed lead is a strange thing to lead a README with.
echo "==> marking a few read"
for rank in 4 9 15; do
  id=$(printf '%s' "$edition" | jget "d.items[$rank] && d.items[$rank].id")
  api -X PUT "$BASE/api/edition/items/$id/read" -o /dev/null
done

echo "==> starting headless chromium"
"$chromium" --headless=new --remote-debugging-port=9222 \
  --user-data-dir="$work/chrome" \
  --no-first-run --no-default-browser-check \
  --hide-scrollbars --force-color-profile=srgb --disable-gpu \
  about:blank > "$work/chromium.log" 2>&1 &
pids+=($!)

echo "==> capturing: front page ${WIDTH}x${HEIGHT}, the rest ${NARROW} wide, ${THEME} theme"
BASE="$BASE" OUT="$OUT" SESSION_COOKIE="$cookie" \
  WIDTH="$WIDTH" HEIGHT="$HEIGHT" NARROW="$NARROW" THEME="$THEME" \
  OPEN_FEED="The Meridian" \
  node "$here/shoot.mjs"

echo "==> done"
ls -la "$OUT"/*.png
