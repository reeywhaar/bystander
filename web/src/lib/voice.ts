/**
 * How one article is made to look unlike the ones around it.
 *
 * Five things vary per card — the face its headline is set in, the size its standfirst is set
 * at, whether it is boxed and how, and whether a wide one runs its body in two columns. A
 * sixth, how many columns of the grid it takes, is the server's and lives in internal/edition.
 *
 * **This is about memory, not decoration.** The page is fixed: it is composed once and does
 * not move until the next one, and the whole point of that is that somebody can come back to
 * an article they half-remember. Identical cards defeat it. A page of fifty things that look
 * the same is a page with one landmark on it, and "it was somewhere in the middle" is all
 * anybody can recall. Give a card an outstanding shape — a boxed story, a headline in the
 * condensed face, a column set larger than its neighbours — and it becomes a thing that can
 * be looked *for* rather than scanned past. Where an article sits is half of finding it
 * again; what it looked like is the other half.
 *
 * Everything here is drawn from one seeded stream per card, and the seed is the edition and
 * the article together. Two consequences, both wanted:
 *
 * - **Stable within a page.** Every draw is a pure function of two ids that do not change
 *   while the page is up, so nothing moves on reload, in a second tab, or after something is
 *   marked read. A landmark that moves is not a landmark.
 * - **New on the next page.** An article that survives into tomorrow's edition is dealt a
 *   different hand, because the edition id changed. Tomorrow is a different page and ought to
 *   look like one — an article boxed in perpetuity would be a fact about the article rather
 *   than about the page it is on.
 *
 * A stream rather than slices of one hash, which is what this was. Six values were cut out of
 * one 32-bit number at hand-chosen offsets, which meant tracking which bits were spent and
 * hoping two draws had not landed on overlapping ones. Successive draws from a generator are
 * independent without anybody having to keep count.
 */

/** The house display faces, defined in styles.css. */
export const VOICES = [
  "didone",
  "antique",
  "workhorse",
  "slab",
  "gothic",
  "humanist",
] as const;

export type Voice = (typeof VOICES)[number];

/**
 * A card's frame, when it has one.
 *
 * Four things vary and they are drawn separately, so a box is not one mark repeated. Nobody is
 * going to catalogue the combinations; the point is that no two boxes on a page are quite the
 * same object, which is what stops the boxed stories blending into each other the way the
 * unboxed ones used to.
 */
export interface Frame {
  /** The line it is drawn in. */
  line: (typeof LINES)[number];
  /** How thick that line is. */
  width: number;
  /** How strongly it is inked, faint to plain. */
  ink: number;
  /** How far the story sits inside it. */
  pad: number;
}

/** The lines a box can be drawn in. */
export const LINES = ["solid", "dashed", "dotted"] as const;

/**
 * What a card is boxed in, as a bag to draw one from — null for the ones that are not.
 *
 * A bag rather than a rate and then a separate draw for the line, because the ratio is
 * something you can count here instead of something you have to work out: four cards in every
 * twenty are boxed, and of those four, half are dashed.
 *
 * One in five, settled by looking at the page rather than by argument. It ran at half for a
 * while — the reasoning being that a box is not one mark, since the line, its weight, its ink
 * and its inset are all drawn separately, so most boxes are visibly different objects. That is
 * true, and it still was not the right number: at half, a box stops being the thing that
 * distinguishes a card, because being boxed is as ordinary as not being boxed. Punctuation
 * only works while most of the page is not punctuated.
 */
const LINE_BAG: (Frame["line"] | null)[] = [
  ...Array<null>(16).fill(null),
  "dashed",
  "dashed",
  "dotted",
  "solid",
];

/** How many sizes a standfirst can be set in. The ladder itself is in styles.css. */
export const PROSE_STEPS = 4;

/** How many steps each of a frame's dimensions has. */
export const FRAME_RANKS = 3;

/**
 * How tall a picture is, as a share of how wide it is — five steps from three fifths to square.
 *
 * Every image was cropped to the same 16:9 before this, which is a television's shape and
 * nothing else's. A page of photographs all cut to one ratio reads as a catalogue: the crop
 * stops being something anybody notices, and it is one of the few things about a picture that
 * carries across a room.
 *
 * Landscape through to square, and no further. A picture here is a crop of somebody else's
 * photograph, and turning a landscape into a portrait would be recomposing a frame its
 * photographer chose. Square is the far end because a square picture still reads as a picture
 * given a shape; a tall one reads as a photograph that has been cut in half.
 *
 * The values themselves are in styles.css, as `.shot-0` upwards.
 */
export const SHOT_STEPS = 5;

/**
 * Whether a picture fills its shape or sits inside it, as a bag to draw from.
 *
 * Two honest answers to the same question, which is why this varies rather than picking one.
 * `cover` fills the crop and loses the edges of the photograph; `contain` keeps the whole
 * photograph and loses some of the space. Neither is right for a picture whose subject nobody
 * here has looked at.
 *
 * Mostly `cover`, because a page is mostly better for it — a filled shape sits in a column
 * cleanly. `contain` is the occasional one that shows a picture whole, which is the more
 * generous treatment of a photograph somebody else framed, and it is the mark that makes a
 * card with a picture in it look unlike the card with a picture beside it.
 */
const FIT_BAG = ["cover", "cover", "cover", "contain"] as const;

export type Fit = (typeof FIT_BAG)[number];

/**
 * How much text is worth splitting into two columns.
 *
 * Three lines cut down the middle is not two columns, it is one paragraph with a gap in it —
 * which was visible the first time this shipped, on a lead whose standfirst was a sentence and
 * a half. Characters rather than lines, because how many lines that is depends on the width,
 * the size drawn from the ladder and the face, none of which this can see.
 */
const COLUMNS_NEED = 600;

/**
 * How often a rule runs across the page, as a chance per card.
 *
 * A newspaper breaks its page into bands with a rule, and the bands are what make a page
 * scannable at arm's length — you find the band first and the story second.
 *
 * A chance rather than a cadence, which is what this was: a rule every five to nine cards put
 * eight of them on a fifty-card page at even intervals, and even intervals are the thing a
 * page is trying not to be. Regular rules read as a table's gridlines; irregular ones read as
 * a compositor deciding a band had gone on long enough.
 *
 * Two per cent, which is one or two rules on a ninety-article page. It was ten per cent to
 * begin with and that was far too many — a rule every ten cards is a rule you stop seeing,
 * and a mark you stop seeing is not dividing anything. This is a break in the page, and a
 * page has one or two of those in it.
 *
 * It does something structural too. Nothing sits beside a full-width element, so a rule closes
 * the band above it and bounds how far `dense` reaches when it backfills — a hole near the top
 * stops being filled by a card from four screens down.
 */
const RULE_CHANCE = 0.02;

/** Everything about how one card looks that is not the server's to decide. */
export interface Style {
  voice: Voice;
  /** Which step of the size ladder the standfirst is set at. */
  prose: number;
  /** Null for most cards: a box is punctuation, and the padding belongs to the box. */
  frame: Frame | null;
  /** Whether a wide card runs its body in two columns. The caller checks the width. */
  columns: boolean;
  /** Whether a rule runs across the page above this card. */
  rule: boolean;
  /** Which step of the crop ladder this card's picture is cut to. */
  shot: number;
  /** Whether that picture fills the crop or sits whole inside it. */
  fit: Fit;
}

/**
 * FNV-1a, 32 bits — used to turn two ids into one seed.
 *
 * Any spreading function would do, and this one is four lines. It matters that it is not
 * `id.length` or the last character: ids are opaque strings that share a prefix, and within
 * one edition they were minted in the same second, so anything reading a fixed position risks
 * reading the part that does not vary.
 */
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  // Unsigned: Math.imul returns a signed 32-bit result, and a negative seed would make the
  // first draw negative too.
  return h >>> 0;
}

/**
 * mulberry32: a small, fast, well-distributed generator.
 *
 * Thirty-two bits of state is plenty for drawing seven small numbers. Written out rather than
 * depended on — it is six lines, and a dependency here would be a dependency in the bundle
 * that has to paint immediately.
 */
function generator(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * How one card looks, on one page.
 *
 * The draws happen in a fixed order and every card makes all of them, including the ones it
 * will not use. A card that skipped its frame draws because it is not boxed would leave every
 * draw after them reading a different number, and "does this one set its body in columns"
 * would secretly depend on whether it happened to be boxed.
 */
export function styleFor(
  editionID: string,
  articleID: string,
  summary = "",
): Style {
  // A separator that cannot appear in an id, so two different pairs cannot concatenate into
  // the same string.
  const next = generator(hash(editionID + " " + articleID));

  const voice = VOICES[Math.floor(next() * VOICES.length)] as Voice;
  const prose = Math.floor(next() * PROSE_STEPS);

  // One draw decides both whether there is a box and what it is drawn in.
  const line = LINE_BAG[Math.floor(next() * LINE_BAG.length)] ?? null;
  const width = Math.floor(next() * FRAME_RANKS);
  const ink = Math.floor(next() * FRAME_RANKS);
  const pad = Math.floor(next() * FRAME_RANKS);

  const columns = next() < 0.5 && summary.length >= COLUMNS_NEED;
  const rule = next() < RULE_CHANCE;
  const shot = Math.floor(next() * SHOT_STEPS);
  const fit = FIT_BAG[Math.floor(next() * FIT_BAG.length)] as Fit;

  return {
    voice,
    prose,
    frame: line ? { line, width, ink, pad } : null,
    columns,
    rule,
    shot,
    fit,
  };
}

/**
 * The voices for a page, with no two in a row the same.
 *
 * That is the one rule a newspaper's headline typography actually has, and an independent draw
 * breaks it about one time in six — which does not read as chance, it reads as the page having
 * failed to notice. It cannot be decided per card, because it is a fact about the sequence;
 * everything else in [Style] can be, and is.
 *
 * Reading order is the server's rank order, which is not exactly what "adjacent" means once a
 * dense grid has filled its holes, but it is what stops a run of three.
 */
export function assignVoices(styles: readonly Style[]): Voice[] {
  let previous: Voice | null = null;

  return styles.map((style) => {
    let voice = style.voice;
    if (voice === previous) {
      // Stepped by the card's own prose draw rather than to the next face along. A fixed +1
      // would turn every collision into the same pair — didone always followed by antique —
      // which is a pattern a reader notices well before they could name it.
      const at = VOICES.indexOf(voice);
      voice = VOICES[(at + 1 + style.prose) % VOICES.length] as Voice;
    }
    previous = voice;
    return voice;
  });
}
