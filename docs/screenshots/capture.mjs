#!/usr/bin/env node
//
// Regenerate the README screenshots.
//
//   docs/screenshots/capture.mjs
//
// Builds the frontend and the binary, starts eight stand-in publishers and a reader against
// them, subscribes to all eight through the reader's own API, composes a page, drives headless
// Chromium over the DevTools protocol, and overwrites the PNGs next to this file. Everything it
// starts is stopped again on the way out, including on failure.
//
// Requires: go, node, and Chromium or Chrome. No Docker, and no npm packages beyond the
// frontend's own — Chromium has a headless mode and Node has fetch and a WebSocket client, so
// there is nothing here for Playwright to do.
//
// This was a bash script. It is Node now for one reason: a shell script that has to parse JSON
// grows a `node -e` helper to do it, and once the awkward half of the work is already in
// JavaScript the shell is only there to hold the easy half badly. Quoting a JSON body through
// two levels of shell escaping is the kind of line nobody can read and everybody has to test by
// running it.
import { spawn, execFileSync } from "node:child_process";
import { accessSync, constants, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as sleep } from "node:timers/promises";

import { connect } from "./cdp.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..", "..");
const work = mkdtempSync(join(tmpdir(), "bystander-shots-"));

// Wide enough for the front page's four columns: the grid drops to three under 1100 and two
// under 820, and a set of screenshots in which the front page is two columns wide sells the
// whole idea short — the layout *is* the product.
const WIDTH = Number(process.env.WIDTH ?? 1240);
// A window, not the whole page — enough of one to show the opener, the column beside it and the
// top of the row below, which between them is every slot the grid has.
const HEIGHT = Number(process.env.HEIGHT ?? 1500);
// The management pages are `max-w-3xl`, so this is that plus a margin.
const NARROW = Number(process.env.NARROW ?? 880);
// How tall the front page believes its window is, which is not how much of it is photographed.
// An ordinary laptop window, because the `vh` bound on a picture's height is measured against
// it — tell the page it is 1500 tall and every capped picture comes out half again too big.
const VIEW = Number(process.env.VIEW ?? 900);
// The theme the committed PNGs are in, so a plain run reproduces them rather than replacing the
// set with the other one.
const THEME = process.env.THEME ?? "light";
// Where the PNGs land. Overridable so that a look at the other theme does not have to overwrite
// the committed set to be worth taking.
const OUT = process.env.OUT ?? here;

// The listen port is not configurable — it is :80 inside a container and the operator remaps it
// — so this has to be able to bind :80. Fine on macOS; on Linux it wants either root or
// `sudo setcap cap_net_bind_service=+ep` on the built binary.
const BASE = "http://localhost";
const STUB = "http://127.0.0.1:8811";

const children = [];
let cleaned = false;

function cleanup() {
  if (cleaned) return;
  cleaned = true;
  for (const child of children) {
    try {
      child.kill("SIGTERM");
    } catch {
      /* already gone */
    }
  }
  // Chromium writes its profile out as it exits, so removing the directory immediately races it
  // and leaves "Directory not empty" noise on the way out.
  try {
    rmSync(work, { recursive: true, force: true, maxRetries: 5, retryDelay: 300 });
  } catch {
    /* it is a temp directory; the OS will get it */
  }
}
process.on("exit", cleanup);
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    cleanup();
    process.exit(1);
  });
}

const step = (message) => console.log(`==> ${message}`);
const die = (message, detail) => {
  console.error(message);
  if (detail) console.error(detail);
  cleanup();
  process.exit(1);
};

/** Run a command to completion, throwing with its output if it fails. */
function run(command, args, options = {}) {
  try {
    execFileSync(command, args, { stdio: "pipe", ...options });
  } catch (err) {
    const output = [err.stdout, err.stderr].filter(Boolean).join("\n");
    die(`${command} ${args.join(" ")} failed`, output);
  }
}

/** Start a long-running process, remembered so cleanup can stop it. */
function start(command, args, options = {}) {
  const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"], ...options });
  const log = [];
  child.stdout?.on("data", (d) => log.push(d));
  child.stderr?.on("data", (d) => log.push(d));
  child.log = () => Buffer.concat(log).toString();
  children.push(child);
  return child;
}

/** Poll until `check` resolves truthy, or give up. */
async function waitFor(check, { attempts = 60, every = 250 } = {}) {
  for (let i = 0; i < attempts; i++) {
    try {
      if (await check()) return true;
    } catch {
      /* not up yet */
    }
    await sleep(every);
  }
  return false;
}

const chromium = [
  "/Applications/Chromium.app/Contents/MacOS/Chromium",
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
  "/usr/bin/google-chrome",
].find((candidate) => {
  try {
    accessSync(candidate, constants.X_OK);
    return true;
  } catch {
    return false;
  }
});
if (!chromium) die("no Chromium or Chrome found; put one on PATH");

// --- build ------------------------------------------------------------------------------

step("building the frontend");
run("npm", ["ci"], { cwd: join(root, "web") });
run("npm", ["run", "build"], { cwd: join(root, "web") });

step("building the binary");
const binary = join(work, "bystander");
run("go", ["build", "-o", binary, "."], { cwd: root });

// --- the stand-in publishers ------------------------------------------------------------

step("starting the stand-in publishers");
const stub = start("node", [join(here, "stub.mjs")]);

// Verify they are *ours*. A stub left running from an earlier attempt keeps the port, the new
// one dies with EADDRINUSE where nothing notices, and the capture then quietly succeeds against
// stale content — which is exactly how a set of screenshots ends up showing something the code
// no longer does.
const publishing = await waitFor(async () => {
  const res = await fetch(`${STUB}/f/meridian.xml`);
  return res.ok && (await res.text()).includes("The Meridian");
});
if (!publishing) {
  die(`the stand-in publishers never answered on :8811 — is something else on that port?`, stub.log());
}

// --- the reader -------------------------------------------------------------------------

const env = { ...process.env, BYSTANDER_PUBLIC_URL: BASE, BYSTANDER_DATA_DIR: join(work, "data") };

// Minted before `serve` starts, deliberately. There is no default account at any point: the
// only way in is an invitation, and bootstrap mints one on an empty database — so doing it here
// means there is exactly one live invitation rather than two.
step("minting an invitation");
const invite = execFileSync(binary, ["invite"], { env, encoding: "utf8" }).split("\n")[0].trim();
const token = invite.slice(invite.lastIndexOf("/") + 1);
if (!token) die("could not mint an invitation", invite);

step("starting the reader");
const reader = start(binary, ["serve"], { env });

const up = await waitFor(async () => (await fetch(`${BASE}/healthz`)).ok);
if (!up) die("the reader never came up — is something else on :80?", reader.log());

// Accepting an invitation signs the new account in, so this one request is also the login.
const accepted = await fetch(`${BASE}/api/invites/${token}/accept`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ username: "demo", password: "demopassword123" }),
});
if (!accepted.ok) die(`accepting the invitation answered ${accepted.status}`, reader.log());

const setCookie = accepted.headers.getSetCookie().find((c) => c.startsWith("bystander_auth="));
const cookie = setCookie?.slice("bystander_auth=".length).split(";")[0];
if (!cookie) die("the invitation was accepted but no session cookie came back");

/**
 * One API call as the demo account, parsed.
 *
 * Throws on anything but a 2xx rather than returning it. Every call here is a setup step whose
 * failure makes every later step meaningless — a feed that did not subscribe is the difference
 * between a front page and an empty state, and it is better to hear about that here than to
 * read it in the README.
 */
async function api(path, { method = "GET", body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      Cookie: `bystander_auth=${cookie}`,
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
  const text = await res.text();
  if (!res.ok) die(`${method} ${path} answered ${res.status}`, text);
  return text ? JSON.parse(text) : null;
}

// --- the corpus -------------------------------------------------------------------------

// One entry per section of the paper, and the feeds filed under it. Priorities are a spread
// rather than all fifty, because a screenshot of every slider at the default says nothing about
// what the sliders are for.
//
// The Wire Desk is at 35 with forty-two articles in it, which is the case the sampler exists
// for: a draw picks a feed before it picks an article, so volume buys nothing.
const SECTIONS = [
  { name: "World", priority: 60, feeds: { meridian: 75, wiredesk: 35 } },
  { name: "Business", priority: 45, feeds: { ledger: 55 } },
  { name: "Technology", priority: 70, feeds: { copperwire: 80 } },
  { name: "Science", priority: 55, feeds: { fieldnotes: 60 } },
  { name: "Culture", priority: 60, feeds: { undercurrent: 65, slowcraft: 50 } },
  { name: "Local", priority: 40, feeds: { harbour: 45 } },
];

step("filing the sections and subscribing");
for (const section of SECTIONS) {
  const tag = await api("/api/tags", {
    method: "POST",
    body: { name: section.name, priority: section.priority },
  });
  for (const [slug, priority] of Object.entries(section.feeds)) {
    await api("/api/feeds", {
      method: "POST",
      body: { url: `${STUB}/f/${slug}.xml`, priority, tag_ids: [tag.id] },
    });
  }
  console.log(`  ${section.name.padEnd(12)} ${Object.keys(section.feeds).join(" ")}`);
}

// Fetching is a job kind and its refill runs on a cadence, so a feed added just after a sweep
// waits for the next one. Without this wait every row on the feeds page says "not fetched yet"
// underneath a feed whose articles are already on the front page. Adding a feed saves the
// articles that discovery already parsed — which is why there is a page at all — but it is the
// fetch job that records a successful fetch.
step("waiting for the first fetch");
const total = (await api("/api/feeds")).length;
const fetched = await waitFor(
  async () => (await api("/api/feeds")).filter((f) => f.last_success_at).length === total,
  { attempts: 60, every: 2000 },
);
if (!fetched) {
  const got = (await api("/api/feeds")).filter((f) => f.last_success_at).length;
  die(`only ${got} of ${total} feeds were ever fetched`, reader.log());
}

// --- the pages --------------------------------------------------------------------------

// Twenty-eight rather than the default: enough that the roughly-one-in-four rule puts several
// wide cards down the page rather than one at the top, which is the shape the grid was designed
// around. Under about twenty the page is an opener and a field of columns.
step("setting the page size");
const front = (await api("/api/pages")).find((p) => p.is_main);
await api(`/api/pages/${front.id}`, { method: "PATCH", body: { edition_size: 28 } });

// A second front page, so the screenshots show the thing rather than describing it.
//
// Culture weekly and short, which is the case the feature is for: a section worth reading
// through on a Sunday, kept out of the daily page's way without being unfollowed.
//
// Saving a filter composes the page, so this needs no separate re-roll: see the API's
// patchPage, which recomposes when what a page draws from changes.
step("making a second front page");
const cultureTag = (await api("/api/tags")).find((t) => t.name === "Culture");
const culture = await api("/api/pages", { method: "POST", body: { name: "Culture", slug: "culture" } });
await api(`/api/pages/${culture.id}`, {
  method: "PATCH",
  body: { include_tag_ids: [cultureTag.id], edition_interval: 604800, edition_size: 12 },
});
const cultureItems = (await api("/api/edition?page=culture")).items.length;
if (cultureItems === 0) die("the Culture page composed nothing; the tab strip would show an empty page");
console.log(`  Culture: ${cultureItems} articles`);

// --- the pictures -----------------------------------------------------------------------
//
// Started before the page is composed, not after, because the composition loop below has a
// question only a browser can answer.
step("starting headless chromium");
start(chromium, [
  "--headless=new",
  "--remote-debugging-port=9222",
  `--user-data-dir=${join(work, "chrome")}`,
  "--no-first-run",
  "--no-default-browser-check",
  "--hide-scrollbars",
  "--force-color-profile=srgb",
  "--disable-gpu",
  "about:blank",
]);
const page = await connect(9222);
await page.send("Page.enable");
await page.send("Network.enable");
await page.send("Network.setCookie", {
  name: "bystander_auth",
  value: cookie,
  domain: "localhost",
  path: "/",
});

// What the page should open on, and why each half of it matters.
//
// **A `wide`.** It is one of the three widths the opener is drawn from, and the only one that
// shows what the grid is for. A `lead` takes all sixteen tracks, so nothing sits beside it: the
// shot becomes one full-width picture and then a row starting below it. A `wide` takes twelve
// and leaves four, which only a column can fill, so `dense` reaches past the next article to
// find one — that backfill is the whole mechanism, and a screenshot of the layout should show
// the layout doing something.
//
// **Carrying the arcs.** The opener's picture is the largest thing in the shot by a wide
// margin, so which of the four compositions it draws is most of the shot's character. The arcs
// carry at that size; the halftone reads as texture and the planes as a wash, both of which are
// fine on a quarter-page card and thin over half a page.
//
// **With its picture above the story, not beside it.** Whether a card sets its picture as an
// aside is drawn in the browser from the edition's id and the article's — the server does not
// know and cannot be asked. Two fifths of a card is a perfectly good picture and a poor hero,
// so the opener that has one is not the card to open a README with.
//
// That last one is why the browser is already running: this asks the page what it drew rather
// than recomputing the draw here, which would be a second copy of a rule in `voice.ts` and
// would go quietly wrong the first time one of them changed.
//
// `regenerate` is a re-roll by design: unread articles go back into the pool first, so this
// spends nothing and is exactly what the reader's own "Make a different page" button does.
const plates = await (await fetch(`${STUB}/plates.json`)).json();
const plateOf = (item) => plates[/\/img\/([^/]+)\.svg$/.exec(item?.image_url ?? "")?.[1] ?? ""];

/** How the browser laid the opening card out, once it has. */
async function openerIsAHero() {
  const loaded = page.once("Page.loadEventFired");
  await page.send("Page.navigate", { url: `${BASE}/` });
  await loaded;
  for (let i = 0; i < 40; i++) {
    const drawn = await page.evaluate(`(() => {
      const card = document.querySelector(".page-grid article");
      return card ? !card.classList.contains("card-aside") : null;
    })()`);
    if (drawn !== null) return drawn;
    await sleep(150);
  }
  return false;
}

/** Re-roll until the page satisfies `want`, or give up and say so. */
async function compose(want, attempts, describe) {
  for (let attempt = 1; attempt <= attempts; attempt++) {
    await api("/api/edition/regenerate", { method: "POST" });
    const drawn = await api("/api/edition");
    const first = drawn.items[0];
    const shape = `${first?.slot}/${plateOf(first) ?? "no picture"}`;
    if ((await want(first)) === true) return drawn;
    console.log(`  re-rolling: opened with a ${shape} (${attempt})`);
  }
  console.error(`  no page in ${attempts} draws ${describe}`);
  return null;
}

// All three, then two, then one, then whatever came out — falling back rather than insisting.
// Every one of these is a real page, and a set of screenshots that could not be regenerated
// because a draw would not co-operate is worse than one that opens on a halftone.
step("composing a page");
const wide = (i) => i?.slot === "wide";
const arcs = (i) => plateOf(i) === "arcs";
let edition =
  (await compose(async (i) => wide(i) && arcs(i) && (await openerIsAHero()), 40, "opened wide on the arcs, above the story")) ??
  (await compose(async (i) => wide(i) && (await openerIsAHero()), 20, "opened wide, above the story")) ??
  (await compose(async (i) => wide(i), 10, "opened wide")) ??
  (await api("/api/edition"));

const first = edition.items[0];
// A brief means the first article had neither a picture nor a standfirst, which is honest
// behaviour and a poor first impression of a layout the README is there to show.
console.log(`  opened with a ${first?.slot}/${plateOf(first) ?? "no picture"}`);

// Three read articles, so the front page shows what "read" looks like — greyed and desaturated,
// in place — and so Recently read has something in it. Not the first three: a greyed opener is a
// strange thing to lead a README with.
step("marking a few read");
for (const rank of [4, 9, 15]) {
  const item = edition.items[rank];
  if (item) await api(`/api/edition/items/${item.id}/read`, { method: "PUT" });
}

page.close();

step(`capturing: front page ${WIDTH}x${HEIGHT} in a ${VIEW}-tall window, the rest ${NARROW} wide, ${THEME} theme`);
run("node", [join(here, "shoot.mjs")], {
  stdio: "inherit",
  env: {
    ...process.env,
    BASE,
    OUT,
    SESSION_COOKIE: cookie,
    WIDTH: String(WIDTH),
    HEIGHT: String(HEIGHT),
    VIEW: String(VIEW),
    NARROW: String(NARROW),
    THEME,
    OPEN_FEED: "The Meridian",
  },
});

step("done");
cleanup();
