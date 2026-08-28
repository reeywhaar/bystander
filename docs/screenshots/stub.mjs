// Eight stand-in publishers on :8811, for the README screenshots.
//
// Invented papers with invented stories. Nothing here is a real publication or a real
// event, and that is deliberate rather than lazy: a screenshot showing somebody else's
// headline is republishing it, and one showing an invented headline attributed to a real
// masthead is worse than that.
//
// Node stdlib only, no dependencies. It serves three things:
//
//   /f/<slug>.xml   an RSS 2.0 feed
//
// A paper's `site` is the address it claims to live at, and deliberately not this server.
// It is the channel's <link>, which becomes the subscription's site_url — the line the
// feeds list prints under every name. Served from here it read as 127.0.0.1:8811 in the
// screenshots. The item links stay on this origin, because a card still has to resolve to
// something. `.example` is the TLD reserved for the purpose: a shot showing a domain
// somebody owns points readers at a stranger.
//   /<slug>/<id>    an article page, so a card's link resolves to something
//   /img/<id>.svg   the picture on a card
//
// The pictures are drawn here rather than committed, because the alternative is either a
// licence to keep track of or a folder of photographs that have nothing to do with this
// project. They are abstract plates in the reader's own two colours, and they carry their
// own prefers-color-scheme block: an SVG in an <img> is a document of its own and honours
// it, so the whole set works in either theme.
import { createServer } from "node:http";

const PORT = 8811;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

/**
 * Publication dates are offsets from the moment this starts, not fixed timestamps, so the
 * cards say "3 hours ago" rather than a date in the past that gets further away every time
 * somebody looks at these files. Every feed's window defaults to a week, so everything here
 * has to be inside one.
 */
const startedAt = Date.now();
const ago = (ms) => new Date(startedAt - ms).toUTCString();

/**
 * The papers. `reach` is only how many filler items a feed carries beyond the ones written
 * out below — The Wire Desk has forty because a prolific publisher taking the page is the
 * thing the sampler exists to prevent, and a screenshot is a fair place to show that it
 * does not.
 */
const PAPERS = [
  {
    slug: "meridian",
    site: "https://meridian.example",
    title: "The Meridian",
    description: "Foreign affairs, at the pace they actually move",
    items: [
      {
        id: "border-crossing",
        title: "The crossing that reopened, and the four towns still waiting on it",
        at: 40 * MINUTE,
        summary:
          "<p>Freight has moved through the northern gate since Tuesday. The villages that " +
          "depend on the southern one have been told to expect a decision <em>within the " +
          "quarter</em> — the third quarter in which that sentence has been said.</p>",
      },
      {
        id: "water-treaty",
        title: "Three governments, one river, and a treaty nobody wants to reopen",
        at: 5 * HOUR,
        summary:
          "<p>The 1974 allocation assumed a flow that has not been measured since 2011. " +
          "Everyone downstream knows it. Nobody upstream will say so first.</p>",
      },
      {
        id: "election-quiet",
        title: "A quiet election, and what that costs",
        at: 19 * HOUR,
        summary:
          "<p>Turnout fell for the fourth cycle running. The parties that gained are the ones " +
          "that stopped campaigning in the districts they had already lost.</p>",
      },
      { id: "ministry-note", title: "Ministry confirms the delegation left on Sunday", at: 26 * HOUR },
      {
        id: "port-strike",
        title: "The port strike enters its second week",
        at: 2 * 24 * HOUR,
        summary: "<p>Nine hundred containers, and a mediator who has not been in the room since Thursday.</p>",
      },
    ],
  },
  {
    slug: "ledger",
    site: "https://ledgerandline.example",
    title: "Ledger & Line",
    description: "Markets, plainly",
    items: [
      {
        id: "market-floor",
        title: "A quiet market finds its floor",
        at: 2 * HOUR,
        summary:
          "<p>Three sessions of nothing much is not a recovery, but it is the first week since " +
          "spring that nobody has had to explain a number.</p>",
      },
      {
        id: "warehouse-lease",
        title: "Everybody built warehouses. Now everybody has warehouses",
        at: 8 * HOUR,
        summary:
          "<p>Industrial rents in the corridor have fallen for two quarters. The buildings that " +
          "went up on the assumption they would not are the ones sitting empty.</p>",
      },
      {
        id: "audit-season",
        title: "What the audit season is quietly repricing",
        at: 22 * HOUR,
        summary:
          "<p>A change in how one line is disclosed has moved more balance sheets this month " +
          "than any of the announcements did.</p>",
      },
      { id: "rate-hold", title: "Rates held, again", at: 30 * HOUR },
      {
        id: "small-lenders",
        title: "The small lenders nobody stress-tests",
        at: 3 * 24 * HOUR,
        summary: "<p>Below the threshold, above the exposure that would have mattered in 2008.</p>",
      },
    ],
  },
  {
    slug: "copperwire",
    site: "https://copperwire.example",
    title: "Copper Wire",
    description: "How the machines actually work",
    items: [
      {
        id: "cache-invalidation",
        title: "We deleted the cache and the latency went down",
        at: 90 * MINUTE,
        summary:
          "<p>A write-through layer added in 2019 to absorb a load pattern that stopped existing " +
          "in 2021. Removing it took an afternoon; being allowed to remove it took nine months.</p>",
      },
      {
        id: "protocol-rot",
        title: "The protocol everyone implements and nobody reads",
        at: 6 * HOUR,
        summary:
          "<p>Four clients, four interpretations of the same paragraph, and a working group that " +
          "has been drafting the clarification since 2016.</p>",
      },
      {
        id: "one-binary",
        title: "In defence of the single binary",
        at: 16 * HOUR,
        summary:
          "<p>Nine services became one. The diagram got boring, the on-call rota got quiet, and " +
          "nobody has needed the diagram since.</p>",
      },
      { id: "cert-expiry", title: "It was the certificate", at: 27 * HOUR },
      {
        id: "index-scan",
        title: "A query plan is not an opinion",
        at: 2 * 24 * HOUR + 4 * HOUR,
        summary: "<p>Six hundred milliseconds, one missing composite index, and a very long meeting.</p>",
      },
    ],
  },
  {
    slug: "fieldnotes",
    site: "https://fieldnotes.example",
    title: "Field Notes",
    description: "Working science, from the people doing it",
    items: [
      {
        id: "moth-count",
        title: "Forty years of counting moths in one garden",
        at: 3 * HOUR,
        summary:
          "<p>One trap, one notebook, one street. The dataset nobody funded is now the longest " +
          "continuous record in the county — and the only one that goes back far enough to argue with.</p>",
      },
      {
        id: "peat-core",
        title: "What the peat core remembers about the bad summers",
        at: 11 * HOUR,
        summary:
          "<p>Two metres down, the pollen changes. Above it, three centuries of a farming pattern " +
          "that ended in a decade.</p>",
      },
      {
        id: "replication",
        title: "The result replicated. The explanation did not",
        at: 25 * HOUR,
        summary:
          "<p>Eleven labs, the same effect, and no two of them describing the mechanism the same way.</p>",
      },
      { id: "telescope-time", title: "Telescope time awarded for the southern survey", at: 34 * HOUR },
    ],
  },
  {
    slug: "undercurrent",
    site: "https://undercurrent.example",
    title: "The Undercurrent",
    description: "Arts, with the argument left in",
    items: [
      {
        id: "archive-kept",
        title: "What the archive kept, and what the museum did not",
        at: 4 * HOUR,
        summary:
          "<p>Two institutions, one estate, and a cataloguing decision made in 1968 that determined " +
          "which half of a life is now considered the work.</p>",
      },
      {
        id: "second-album",
        title: "The difficult second album, forty years on",
        at: 13 * HOUR,
        summary:
          "<p>It sold badly, it was reviewed badly, and it is the only one of the three anybody " +
          "still puts on.</p>",
      },
      {
        id: "restoration",
        title: "A restoration that admits it is one",
        at: 23 * HOUR,
        summary:
          "<p>The new panels are a shade lighter than the old, on purpose, and visitors keep asking " +
          "about the seam — which is the point.</p>",
      },
      { id: "gallery-closes", title: "The east wing closes for the winter", at: 2 * 24 * HOUR + 2 * HOUR },
    ],
  },
  {
    slug: "harbour",
    site: "https://harbourgazette.example",
    title: "Harbour Gazette",
    description: "This town, this week",
    items: [
      {
        id: "dredging",
        title: "The dredging nobody voted for",
        at: 7 * HOUR,
        summary:
          "<p>Two consultations, one of them held in August, and a contract signed the week before " +
          "the third was announced.</p>",
      },
      {
        id: "bus-route",
        title: "The 14 will run on Sundays again",
        at: 20 * HOUR,
        summary: "<p>From the first of next month, and only as far as the hospital.</p>",
      },
      { id: "market-hall", title: "Market hall roof: still the roof", at: 29 * HOUR },
      {
        id: "swimming",
        title: "Somebody has been swimming in the harbour all winter",
        at: 3 * 24 * HOUR,
        summary: "<p>Forty-one consecutive weeks, four degrees, and a growing number of witnesses.</p>",
      },
    ],
  },
  {
    slug: "wiredesk",
    site: "https://wiredesk.example",
    title: "The Wire Desk",
    description: "Everything, as it lands",
    reach: 40,
    items: [
      { id: "wire-lead", title: "Delegation arrives ahead of Thursday's session", at: 55 * MINUTE },
      { id: "wire-two", title: "Regulator opens consultation on disclosure rules", at: 4 * HOUR },
    ],
  },
  {
    slug: "slowcraft",
    site: "https://slowcraft.example",
    title: "Slow Craft",
    description: "Once a month, if that",
    items: [
      {
        id: "handplane",
        title: "Sharpening, and the eleven years it took to stop being interesting",
        at: 2 * 24 * HOUR + 6 * HOUR,
        summary:
          "<p>A jig, then a better jig, then no jig. The shaving got thinner every year and the " +
          "ritual got shorter every year, and only one of those was the goal.</p>",
      },
      {
        id: "dovetail",
        title: "Cut it twice",
        at: 5 * 24 * HOUR,
        summary: "<p>Not measured twice. Cut twice — the second one on the offcut, before it matters.</p>",
      },
    ],
  },
];

// ---------------------------------------------------------------------------
// The plates.
// ---------------------------------------------------------------------------

/** mulberry32, so a slug always draws the same picture. */
function seeded(text) {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  let a = h >>> 0;
  const next = () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  // Warm it up. FNV over eight similar slugs gives eight nearby seeds, and mulberry32's
  // first output tracks its seed closely enough that "pick one of four compositions" came
  // out as three halftones in a row on the actual page.
  for (let i = 0; i < 8; i++) next();
  return next;
}

/**
 * One abstract plate: four families of composition, chosen by the seed.
 *
 * Square, 1200×1200, for every card whatever its slot. One image per aspect ratio would be
 * work in service of a crop nobody can see — but the *source* shape is not nothing, because
 * `object-cover` scales before it crops. A 16:9 plate in the nearly-square box the shot ladder
 * often draws has to be scaled up about a quarter to cover it, so what a big card showed was a
 * magnified fragment of the middle: the strokes came out coarse and the composition was mostly
 * off-frame. Square is the shape that never has to be enlarged, whatever the ladder draws,
 * because it is at least as tall as it is wide and the cards are never taller than square.
 *
 * Kept deliberately faint. The first version of these was op-art — dense, high-contrast,
 * and the only thing on the front page anybody looked at, which is the exact opposite of
 * what a picture on a newspaper page does. A plate stands in for a photograph, and a
 * photograph does not out-shout the headline beside it.
 */
function plate(id) {
  const rnd = seeded(id);
  const pick = (list) => list[Math.floor(rnd() * list.length)];
  const W = 1200;
  const H = 1200;
  const shapes = [];

  // Which composition, taken from the article's position in the corpus rather than from its
  // seed. Four families and a hash means three of the four cards above the fold come out the
  // same one about a fifth of the time, and it looked far more like a bug than like chance —
  // the same reason the reader assigns headline faces over the sequence instead of per
  // article. Everything *within* the composition is still seeded, so no two are alike.
  const family = (FAMILY.get(id) ?? Math.floor(rnd() * 4)) % 4;

  if (family === 0) {
    // Concentric arcs off one corner.
    const cx = pick([0, W]);
    const cy = pick([0, H]);
    for (let i = 8; i >= 1; i--) {
      shapes.push(
        `<circle cx="${cx}" cy="${cy}" r="${Math.round((i / 8) * W * 0.85)}" ` +
          `fill="none" stroke="${i % 3 === 0 ? "var(--a)" : "var(--i)"}" ` +
          `stroke-width="${Math.round(3 + rnd() * 11)}" opacity="${(0.1 + i * 0.03).toFixed(2)}"/>`,
      );
    }
  } else if (family === 1) {
    // A halftone field, thinning across the plate — in one of four directions, because two
    // of these side by side are otherwise the same picture at a different pitch.
    const step = 40 + Math.floor(rnd() * 18);
    const axis = Math.floor(rnd() * 4);
    for (let y = step / 2; y < H; y += step) {
      for (let x = step / 2; x < W; x += step) {
        const fade =
          axis === 0 ? 1 - x / W : axis === 1 ? x / W : axis === 2 ? 1 - y / H : y / H;
        const r = Math.max(0, (step / 5) * (fade * 0.9 + rnd() * 0.2));
        if (r < 0.6) continue;
        shapes.push(
          `<circle cx="${x.toFixed(0)}" cy="${y.toFixed(0)}" r="${r.toFixed(1)}" ` +
            `fill="${rnd() > 0.94 ? "var(--a)" : "var(--i)"}"/>`,
        );
      }
    }
  } else if (family === 2) {
    // Overlapping planes, tilted off the axes.
    //
    // This was stacked horizontal bars — "a column of type seen from too far away to read" —
    // and the idea did not survive contact with a card the width of the page. Left-aligned
    // grey bars of ragged length are what every loading skeleton in the world looks like, so
    // the plate that was standing in for a photograph read as a photograph that had not
    // arrived yet. A screenshot cannot afford that: the whole claim of these pictures is that
    // they are pictures.
    //
    // Tilted is most of the fix. Nothing in an interface sits at seven degrees, so a rotated
    // plane cannot be mistaken for a component however grey it is, and overlapping them at
    // different opacities gives the depth a flat stack of bars never had.
    // Smaller and more numerous than the first attempt, which used three or four planes at
    // half the plate each and came out as a wash: at the size the opening card is drawn, a
    // composition of four flat shapes has nothing in it to look at twice. Detail has to
    // survive being enlarged, so most of these are outlines rather than fills — a stroke keeps
    // its edge at any scale, and a 12% grey rectangle at half a page is fog.
    const planes = 6 + Math.floor(rnd() * 4);
    for (let i = 0; i < planes; i++) {
      const w = W * (0.14 + rnd() * 0.3);
      const h = H * (0.14 + rnd() * 0.3);
      // Placed so some of them run off the plate. A composition whose every element is
      // comfortably inside the frame reads as a diagram of a composition.
      const x = -W * 0.12 + rnd() * W;
      const y = -H * 0.12 + rnd() * H;
      const angle = (rnd() * 2 - 1) * 16;
      // Roughly one in three is filled; the rest are drawn. Nothing in an interface sits at
      // sixteen degrees, which is most of why this can no longer be mistaken for a skeleton.
      const filled = rnd() > 0.66;
      const ink = i % 5 === 4 ? "var(--a)" : "var(--i)";
      shapes.push(
        `<rect x="${x.toFixed(0)}" y="${y.toFixed(0)}" width="${w.toFixed(0)}" height="${h.toFixed(0)}" ` +
          `transform="rotate(${angle.toFixed(1)} ${(x + w / 2).toFixed(0)} ${(y + h / 2).toFixed(0)})" ` +
          (filled
            ? `fill="${ink}" opacity="${(0.1 + rnd() * 0.18).toFixed(2)}"/>`
            : `fill="none" stroke="${ink}" stroke-width="${(2 + rnd() * 5).toFixed(1)}" ` +
              `opacity="${(0.2 + rnd() * 0.3).toFixed(2)}"/>`),
      );
    }
    // Two hard edges across the whole plate, so the drifting shapes have something to sit
    // against and the eye has somewhere to start.
    for (const at of [0.28 + rnd() * 0.2, 0.62 + rnd() * 0.2]) {
      const ly = H * at;
      shapes.push(
        `<line x1="0" y1="${ly.toFixed(0)}" x2="${W}" y2="${(ly + (rnd() * 2 - 1) * H * 0.14).toFixed(0)}" ` +
          `stroke="var(--i)" stroke-width="${(2 + rnd() * 4).toFixed(1)}" opacity="0.32"/>`,
      );
    }
  } else {
    // A diagonal weave.
    const gap = 30 + Math.floor(rnd() * 24);
    for (let x = -H; x < W + H; x += gap) {
      shapes.push(
        `<line x1="${x}" y1="0" x2="${x + H}" y2="${H}" stroke="var(--i)" ` +
          `stroke-width="${(1.5 + rnd() * 4).toFixed(1)}" opacity="${(0.08 + rnd() * 0.22).toFixed(2)}"/>`,
      );
    }
    const r = 80 + rnd() * 140;
    shapes.push(
      `<circle cx="${(W * (0.25 + rnd() * 0.5)).toFixed(0)}" cy="${(H * (0.25 + rnd() * 0.5)).toFixed(0)}" ` +
        `r="${r.toFixed(0)}" fill="var(--a)" opacity="0.42"/>`,
    );
  }

  // The reader's own palette, and its own dark variant. An <img>-loaded SVG is a separate
  // document with its own media queries, so the plates turn over with the theme instead of
  // sitting on the page as a patch of daylight.
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}">
<style>
  :root { --p: #f2efe6; --i: #1a1917; --a: #8c2f16; }
  @media (prefers-color-scheme: dark) {
    :root { --p: #100f0c; --i: #e9e5da; --a: #d97a5c; }
  }
</style>
<rect width="${W}" height="${H}" fill="var(--p)"/>
<g opacity="0.62">
${shapes.join("\n")}
</g>
</svg>`;
}

// ---------------------------------------------------------------------------
// The feeds.
// ---------------------------------------------------------------------------

const escape = (text) =>
  text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

/**
 * The filler items a prolific feed carries.
 *
 * Deliberately dull and deliberately plentiful: their whole job is to be forty articles the
 * sampler could take and does not, because a draw picks a feed before it picks an article.
 */
function filler(paper, count) {
  const subjects = [
    "Committee publishes its minutes",
    "Second reading scheduled",
    "Quarterly figures released",
    "Statement issued overnight",
    "Talks continue for a third day",
    "Appointment confirmed",
    "Amendment withdrawn",
    "Inquiry extends its deadline",
  ];
  return Array.from({ length: count }, (_, i) => ({
    id: `${paper.slug}-filler-${i}`,
    title: `${subjects[i % subjects.length]} (${i + 1})`,
    at: (i + 3) * 47 * MINUTE,
  }));
}

function feedXML(paper) {
  const items = [...paper.items, ...(paper.reach ? filler(paper, paper.reach) : [])];

  const entries = items
    .map((item) => {
      const link = `${ORIGIN}/${paper.slug}/${item.id}`;
      // No enclosure and no description at all on some items, on purpose: an article with
      // neither is laid out as a brief, and a page of nothing but pictures is not the page
      // this reader actually produces.
      const picture = item.summary
        ? `\n      <enclosure url="${ORIGIN}/img/${item.id}.svg" type="image/svg+xml" length="0"/>`
        : "";
      const summary = item.summary
        ? `\n      <description><![CDATA[${item.summary}]]></description>`
        : "";
      return `    <item>
      <title>${escape(item.title)}</title>
      <link>${link}</link>
      <guid isPermaLink="true">${link}</guid>
      <pubDate>${ago(item.at)}</pubDate>${summary}${picture}
    </item>`;
    })
    .join("\n");

  return `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
  <channel>
    <title>${escape(paper.title)}</title>
    <link>${paper.site}/</link>
    <description>${escape(paper.description)}</description>
${entries}
  </channel>
</rss>
`;
}

const bySlug = new Map(PAPERS.map((p) => [p.slug, p]));

/**
 * Article id to composition family, cycling through the four.
 *
 * Only the written items are in here. A filler item has no standfirst, so it carries no
 * picture, so nothing ever asks for its plate.
 */
const FAMILY = new Map(
  PAPERS.flatMap((paper) => paper.items).map((item, i) => [item.id, i % 4]),
);

/** The four, by the number they are drawn as, so the capture can ask for one by name. */
const FAMILIES = ["arcs", "halftone", "planes", "weave"];

createServer((req, res) => {
  const path = new URL(req.url, ORIGIN).pathname;

  // Which composition each article's plate is drawn in.
  //
  // Published so the capture can insist that the card opening the page carries the arcs — the
  // strongest of the four, and the one worth putting at the size the opener is drawn at. The
  // alternative was for capture.mjs to recompute the family from the id, which means two
  // copies of this rule and a screenshot that quietly stops being what it says it is the first
  // time one of them changes.
  if (path === "/plates.json") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify(Object.fromEntries([...FAMILY].map(([id, n]) => [id, FAMILIES[n]]))),
    );
    return;
  }

  const feed = /^\/f\/([a-z]+)\.xml$/.exec(path);
  if (feed && bySlug.has(feed[1])) {
    res.writeHead(200, { "Content-Type": "application/rss+xml; charset=utf-8" });
    res.end(feedXML(bySlug.get(feed[1])));
    return;
  }

  const img = /^\/img\/([a-z0-9-]+)\.svg$/.exec(path);
  if (img) {
    res.writeHead(200, { "Content-Type": "image/svg+xml; charset=utf-8" });
    res.end(plate(img[1]));
    return;
  }

  // An article page. Never captured, but a card's link has to resolve to something, and a
  // 404 behind every headline is the kind of detail that makes somebody doubt the rest.
  const article = /^\/([a-z]+)\/(.*)$/.exec(path);
  if (article && bySlug.has(article[1])) {
    const paper = bySlug.get(article[1]);
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    res.end(`<!doctype html><meta charset="utf-8"><title>${escape(paper.title)}</title>
<p>A stand-in article, from a stand-in publisher, for the bystander screenshots.</p>`);
    return;
  }

  res.writeHead(404, { "Content-Type": "text/plain" });
  res.end("not here\n");
}).listen(PORT, "127.0.0.1", () => {
  console.log(`stand-in publishers on ${ORIGIN}`);
  for (const paper of PAPERS) {
    const n = paper.items.length + (paper.reach ?? 0);
    console.log(`  /f/${paper.slug}.xml  ${paper.title} (${n} articles)`);
  }
});
