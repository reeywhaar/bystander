/**
 * Which of the house display faces a headline is set in.
 *
 * A newspaper does not set every headline on a page in the same face, and this reader is
 * composing a newspaper page out of feeds that have nothing to do with each other. Six
 * voices, defined in styles.css, and an article gets one of them.
 *
 * **Random, but only once.** The face is a pure function of the article's id, so the same
 * article is in the same face on every load, in every tab, for as long as it is on the
 * page. That is not a detail — the reader's whole layout is fixed server-side precisely so
 * that where an article sits is how somebody remembers where they were, and typography that
 * reshuffled on reload would undo that more thoroughly than moving the cards would.
 *
 * Decided here rather than stored beside the slot, which is the other obvious place. A
 * slot has to be stored: it comes out of a weighted draw that cannot be repeated. A voice
 * is a hash of an id that is already on the page, so storing it would buy the same answer
 * at the price of a column, a migration and an API field.
 */

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
 * FNV-1a, 32 bits.
 *
 * Any spreading function would do, and this one is four lines. It matters that it is not
 * `id.length % 6` or the last character: ids are 26 characters of Crockford base32 over a
 * timestamp and ten random bytes, so anything reading a fixed position risks reading the
 * timestamp — and every article on one page was minted within the same second.
 */
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  // Unsigned: Math.imul returns a signed 32-bit result, and a negative operand to % would
  // give a negative index.
  return h >>> 0;
}

function voiceAt(index: number): Voice {
  // The modulo has already put this in range; the assertion is only what tells TypeScript
  // so, since noUncheckedIndexedAccess cannot know it.
  return VOICES[index % VOICES.length] as Voice;
}

/**
 * How many sizes a standfirst can be set in.
 *
 * The sizes themselves are in styles.css, as `.prose-step-0` upwards — a ladder from a shade
 * under a pixel-16 to a shade over eighteen.
 */
export const PROSE_STEPS = 4;

/**
 * Which step an article's standfirst is set at.
 *
 * **Not tied to how wide the card is**, which is the whole point. Size used to follow the
 * slot: wide cards got the large setting, narrow cards the small one, so every page had
 * exactly two sizes of prose on it arranged largest-first — which is a template, not a page.
 * A compositor does the opposite constantly, setting a single column large for emphasis and
 * a wide feature small and dense, and that mismatch between column width and type size is
 * most of what stops a page reading as a grid somebody filled in.
 *
 * A pure function of the id, exactly as the voice is, and for the same reason: the layout is
 * fixed server-side so that where an article sits is how somebody remembers where they were,
 * and type that resized on reload would undo that.
 *
 * Worked out by the card rather than handed down like the voice, because there is no rule
 * here about neighbours. Two faces in a row would read as the page failing to notice; two
 * standfirsts a step apart read as a page that was set.
 *
 * A different slice of the hash than the voice takes, so a face and a size are not quietly
 * locked together either — every Oswald headline over the same size of prose would be a
 * pattern, and the whole idea is that there is not one.
 */
export function proseStep(id: string): number {
  return (hash(id) >>> 16) % PROSE_STEPS;
}

/**
 * How a card is framed: boxed, or not.
 *
 * Newspapers box a story to set it apart from the columns around it — a sidebar, a standalone
 * item, something that is not part of the flow. It is one of the few marks a page makes that
 * is not type, and a page with none of them is flatter for it.
 *
 * The transparent one is doing real work. Every card carries the same border and the same
 * padding, and only the *paint* changes — so a boxed story sits on exactly the same
 * gridlines as an unboxed one, and boxing costs nothing in alignment. Give the border only
 * to the boxed cards and their text insets by a couple of pixels relative to their
 * neighbours, which reads as a wobble rather than as a box.
 */
export const FRAMES = ["transparent", "solid", "dashed", "dotted"] as const;

export type Frame = (typeof FRAMES)[number];

/**
 * How often a card is boxed at all, as one in this many.
 *
 * Boxing is punctuation. A page where a sixth of the cards are boxed has texture; a page
 * where half of them are has a table.
 */
const BOXED_IN = 6;

/**
 * Which frame a card takes.
 *
 * A pure function of the id, as the voice and the type ladder are, and for the same reason:
 * the layout is fixed so that where an article sits is how somebody remembers where they
 * were, and a box that came and went on reload would undo that.
 *
 * Its own slice of the hash again, so being boxed is not secretly the same fact as being
 * set in Oswald.
 */
export function frameFor(id: string): Frame {
  const h = hash(id) >>> 24;
  if (h % BOXED_IN !== 0) return "transparent";
  // Of the boxed ones, which line. Taken from a different part of the same byte so the
  // three styles are evenly spread among them rather than one being far rarer.
  return FRAMES[1 + (((h / BOXED_IN) | 0) % (FRAMES.length - 1))] as Frame;
}

/**
 * Assign a voice to each article, in the order the server put them in.
 *
 * Returns the articles paired with their voices rather than a bare list, so a caller cannot
 * pair them up wrongly and does not have to index into a second array to render one.
 *
 * **No two in a row share a face.** That is the one rule a newspaper's headline typography
 * actually has, and a plain hash breaks it about one time in six — which does not read as
 * chance, it reads as the page having failed to notice. Reading order is the server's rank
 * order, which is not exactly what "adjacent" means once a dense grid has filled its holes,
 * but it is what stops a run of three.
 */
export function assignVoices<T extends { id: string }>(
  articles: readonly T[],
): { article: T; voice: Voice }[] {
  let previous = -1;

  return articles.map((article) => {
    const h = hash(article.id);
    let index = h % VOICES.length;

    if (index === previous) {
      // Stepped by a second slice of the same hash rather than to the next face along. A
      // fixed +1 would turn every collision into the same pair — didone always followed by
      // antique — which is a pattern a reader notices well before they could name it.
      index = (index + 1 + ((h >>> 8) % (VOICES.length - 1))) % VOICES.length;
    }

    previous = index;
    return { article, voice: voiceAt(index) };
  });
}
