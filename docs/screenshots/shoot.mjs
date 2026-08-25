// Screenshot the reader by driving headless Chromium over the DevTools protocol.
//
// No Playwright and no browser download: Chromium ships its own headless mode and Node has
// a WebSocket client, so the whole driver is the handful of CDP calls below.
//
// Invoked by capture.sh, which starts everything this expects. Environment:
//   BASE            base URL of a running instance    (default http://localhost)
//   SESSION_COOKIE  value of a valid bystander_auth cookie
//   OUT             directory to write PNGs into      (default .)
//   WIDTH           CSS width for the front page      (default 1240)
//   HEIGHT          CSS height for the front page     (default 1120)
//   NARROW          CSS width for everything else     (default 880)
//   THEME           light | dark                      (default light)
//   OPEN_FEED       name of the feed whose dialog is captured
import { writeFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

const BASE = process.env.BASE ?? "http://localhost";
const OUT = process.env.OUT ?? ".";
const COOKIE = process.env.SESSION_COOKIE;
const WIDTH = Number(process.env.WIDTH ?? 1240);
const HEIGHT = Number(process.env.HEIGHT ?? 1120);
const NARROW = Number(process.env.NARROW ?? 880);
const THEME = process.env.THEME ?? "light";
const OPEN_FEED = process.env.OPEN_FEED ?? "The Meridian";

if (!COOKIE) throw new Error("SESSION_COOKIE is required");

const target = await (async () => {
  for (let i = 0; i < 50; i++) {
    try {
      const list = await (await fetch("http://127.0.0.1:9222/json/list")).json();
      const page = list.find((t) => t.type === "page");
      if (page?.webSocketDebuggerUrl) return page;
    } catch {
      /* not up yet */
    }
    await sleep(200);
  }
  throw new Error("chromium devtools endpoint never came up on :9222");
})();

const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((ok, bad) => {
  ws.onopen = ok;
  ws.onerror = bad;
});

let nextId = 1;
const pending = new Map();
const listeners = new Map();

ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.id && pending.has(msg.id)) {
    const { resolve, reject } = pending.get(msg.id);
    pending.delete(msg.id);
    msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
  } else if (msg.method && listeners.has(msg.method)) {
    listeners.get(msg.method).forEach((fn) => fn(msg.params));
    listeners.delete(msg.method);
  }
};

const send = (method, params = {}) =>
  new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params }));
  });

const once = (method) =>
  new Promise((resolve) => {
    if (!listeners.has(method)) listeners.set(method, []);
    listeners.get(method).push(resolve);
  });

async function evaluate(expression) {
  const { result, exceptionDetails } = await send("Runtime.evaluate", {
    expression,
    awaitPromise: true,
    returnByValue: true,
  });
  if (exceptionDetails) throw new Error(`${exceptionDetails.text}\n${expression}`);
  return result.value;
}

/**
 * Poll for a selector rather than sleeping a fixed amount: every page here suspends on its
 * own fetches, and a fixed wait is either flaky or slow.
 *
 * The selector goes in as a variable, never interpolated into the string — one containing a
 * double quote would otherwise turn the whole expression into a parse error, which surfaces
 * as a mysterious immediate failure rather than a timeout.
 */
const waitFor = (selector, timeout = 15000) =>
  evaluate(`
    new Promise((resolve, reject) => {
      const sel = ${JSON.stringify(selector)};
      const deadline = Date.now() + ${timeout};
      (function poll() {
        if (document.querySelector(sel)) return resolve(true);
        if (Date.now() > deadline) return reject(new Error("timeout waiting for " + sel));
        setTimeout(poll, 100);
      })();
    })`);

const clickText = (selector, text) =>
  evaluate(`
    (() => {
      const sel = ${JSON.stringify(selector)}, want = ${JSON.stringify(text)};
      const el = [...document.querySelectorAll(sel)].find(e => e.textContent.trim().includes(want));
      if (!el) throw new Error("no " + sel + " containing " + want);
      el.click();
      return true;
    })()`);

const setViewport = (width, height) =>
  send("Emulation.setDeviceMetricsOverride", {
    width: Math.round(width),
    height: Math.round(height),
    deviceScaleFactor: 2,
    mobile: false,
  });

/**
 * Size the viewport to the content, so a screenshot is not mostly empty page.
 *
 * Shrinks before measuring. A shell with a minimum height never reports a scrollHeight
 * below the current viewport, so measuring at the height we happen to be at just returns
 * that height and the capture comes out padded with blank paper.
 */
async function fitHeight(width, min = 420, max = 2400) {
  await setViewport(width, min);
  await sleep(150);
  const h = await evaluate(
    `Math.max(document.documentElement.scrollHeight, document.body.scrollHeight)`,
  );
  await setViewport(width, Math.min(max, Math.max(min, Math.ceil(h))));
  await sleep(250); // let the layout settle at the new height
}

/**
 * Size the viewport to a dialog's content.
 *
 * A <dialog> is sized against the viewport with its own scroll, so the document's
 * scrollHeight says nothing about how tall its contents are. Grow first so nothing is
 * clipped, then measure where the content actually ends — and measure the children's boxes
 * rather than the scroller's scrollHeight, which collapses to the tall viewport we just set
 * the moment the content stops overflowing it.
 */
async function fitDialogHeight(width, max = 2000) {
  await setViewport(width, max);
  await sleep(350);

  // The dialog is itself the scroller — see Modal.tsx, where `overflow-y-auto` and
  // `max-h-[85dvh]` are on the <dialog> — so its own scrollHeight is the whole of what there
  // is to show, and the viewport has to be taller than that by the same factor for all of it
  // to fit.
  //
  // Not the first descendant that scrolls, which is what this looked for and is a different
  // thing entirely. A dialog can hold a list that scrolls *inside* it on purpose — the tag
  // picker is capped at twelve rem — and measuring to the bottom of that one cuts off
  // everything below it, including the Save button. It happened to work while the only such
  // list was the last thing in the dialog.
  const h = await evaluate(`(() => {
    const dialog = document.querySelector("dialog[open]");
    if (!dialog) return 0;
    return Math.ceil(dialog.scrollHeight / 0.85);
  })()`);

  await setViewport(width, Math.min(max, Math.max(420, h + 48)));
  await sleep(300);
}

async function shot(name) {
  // The headline faces are the point of half these screenshots, and they arrive after the
  // first paint — `font-display: swap` means a shot taken too early is a shot of the
  // fallback serif. Nothing else in this file would have noticed.
  await evaluate("document.fonts.ready.then(() => true)");
  await evaluate("new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))");
  const { data } = await send("Page.captureScreenshot", { format: "png" });
  writeFileSync(`${OUT}/${name}.png`, Buffer.from(data, "base64"));
  console.log(`  wrote ${name}.png`);
}

await send("Page.enable");
await send("Runtime.enable");
await send("Network.enable");

// Headless Chromium reports prefers-color-scheme: light. The reader honours the system
// preference and has a full dark palette, so without this THEME would do nothing.
await send("Emulation.setEmulatedMedia", {
  features: [{ name: "prefers-color-scheme", value: THEME }],
});

const navigate = async (path) => {
  const loaded = once("Page.loadEventFired");
  await send("Page.navigate", { url: BASE + path });
  await loaded;
};

// --- 1. signed out ---
await setViewport(720, 800);
await navigate("/login");
await waitFor('input[autocomplete="username"]');
await sleep(300);
await fitHeight(720);
await shot("login");

// Sign in by setting the cookie rather than typing into the form: fewer moving parts, and
// the form itself is the shot above.
await send("Network.setCookie", {
  name: "bystander_auth",
  value: COOKIE,
  domain: new URL(BASE).hostname,
  path: "/",
  httpOnly: true,
});

// --- 2. the front page ---
//
// A fixed window rather than fitHeight, and it is the one shot where that is right. A page
// of twenty-eight articles is several thousand pixels tall; captured whole it becomes a
// ribbon that a README renders two inches wide. This is what somebody sees when they open
// the thing.
await setViewport(WIDTH, HEIGHT);
await navigate("/");
await waitFor(".page-grid article h2");
await sleep(900); // the plates are lazy-loaded images
await shot("frontpage");

// --- 3. the feeds somebody follows ---
await navigate("/manage");
await waitFor('main input[type="range"]');
await sleep(400);
await fitHeight(NARROW);
await shot("feeds");

// --- 4. one feed's dialog ---
//
// Everything about a feed lives behind its name — the name itself, what it is filed under,
// how far back a page reaches into it, and stopping following it. Worth its own shot,
// because the list deliberately gives no hint that there is a page of settings behind a
// word that looks like a heading.
await clickText("main button", OPEN_FEED);
await waitFor("dialog[open] input");
await sleep(400);
await fitDialogHeight(NARROW);
await shot("feed");
await evaluate(`document.querySelector("dialog[open]")?.close()`);
await sleep(200);

// --- 5. the front pages somebody keeps ---
//
// The tab strip, and the three controls that belong to whichever page is selected: how often
// it is composed, how much is on it, and how current it has to be.
await navigate("/manage/pages");
await waitFor('main input[type="range"]');
await sleep(400);
await fitHeight(NARROW);
await shot("pages");

// --- 6. what a front page draws from ---
//
// The filter, which is the whole of what makes a second page worth having: one list of tags and
// one of feeds, each name on a switch with three positions — left to drop it, right to take it,
// and the middle to say nothing about it. A dialog with one save rather than controls that
// apply as they are touched, because half a filter is a page drawing from the wrong things —
// briefly, and then for a week.
await clickText("main nav button", "Culture");
await sleep(300);
await clickText("main button", "Edit");
await waitFor('dialog[open] [role="radiogroup"]');
await sleep(400);
await fitDialogHeight(NARROW);
await shot("page");
await evaluate(`document.querySelector("dialog[open]")?.close()`);
await sleep(200);

// --- 7. what has already been read ---
//
// The one list in the product, and the argument for why it is not an unread count in
// disguise: it counts nothing and holds only what somebody has finished with. capture.sh marks
// a few articles read so there is something here.
await navigate("/manage/read");
await waitFor("main ul li a");
await sleep(400);
await fitHeight(NARROW);
await shot("read");

ws.close();
console.log("done");
process.exit(0);
