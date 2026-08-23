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
