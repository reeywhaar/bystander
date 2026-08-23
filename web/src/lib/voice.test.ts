import { describe, expect, it } from "vitest";

import {
  assignVoices,
  frameFor,
  PROSE_STEPS,
  proseStep,
  VOICES,
  type Frame,
  type Voice,
} from "@app/lib/voice";

/** Ids in the shape ids.go mints them: a prefix, a shared millisecond, a random tail. */
function ids(count: number): { id: string }[] {
  const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
  return Array.from({ length: count }, (_, i) => {
    let tail = "";
    for (
      let n = i * 2654435761;
      tail.length < 10;
      n = Math.imul(n + 1, 40503)
    ) {
      tail += alphabet[(n >>> 11) % alphabet.length];
    }
    return { id: `a_01JQK7WZ8M${tail}TSVWXYZ`.slice(0, 28) };
  });
}

describe("assignVoices", () => {
  // The reader's whole layout is fixed server-side so that where an article sits is how
  // somebody remembers where they were. Typography that reshuffled on reload would undo
  // that more thoroughly than moving the cards would.
  it("gives one article the same face every time", () => {
    const page = ids(24);
    const first = assignVoices(page).map((a) => a.voice);
    const again = assignVoices(page).map((a) => a.voice);
    expect(again).toEqual(first);
  });

  // The one rule a newspaper's headline typography actually has.
  it("never sets two headlines in a row in the same face", () => {
    const assigned = assignVoices(ids(200)).map((a) => a.voice);
    const runs = assigned.filter(
      (voice, i) => i > 0 && voice === assigned[i - 1],
    );
    expect(runs).toEqual([]);
  });

  it("uses the whole set rather than favouring a corner of it", () => {
    const counts = new Map<Voice, number>(VOICES.map((v) => [v, 0]));
    for (const { voice } of assignVoices(ids(600))) {
      counts.set(voice, (counts.get(voice) ?? 0) + 1);
    }
    // A sixth of 600 is 100. Generous bounds: this is asserting that no face is unreachable
    // or nearly compulsory, not that a hash is a uniform random source.
    for (const [voice, count] of counts) {
      expect(count, voice).toBeGreaterThan(40);
      expect(count, voice).toBeLessThan(200);
    }
  });

  it("pairs each voice with the article it belongs to, in the server's order", () => {
    const page = ids(5);
    const assigned = assignVoices(page);
    expect(assigned.map((a) => a.article)).toEqual(page);
  });

  it("has nothing to say about an empty page", () => {
    expect(assignVoices([])).toEqual([]);
  });
});

describe("proseStep", () => {
  it("is the same every time, so type does not resize under a reader", () => {
    const id = "a_06G30HM6D10P236HGJDFTSHXFW";
    expect(proseStep(id)).toBe(proseStep(id));
  });

  it("uses the whole ladder", () => {
    // Every step reachable, or the range is narrower than the stylesheet claims.
    const seen = new Set(
      Array.from({ length: 400 }, (_, i) => proseStep("a_" + i)),
    );
    expect(seen.size).toBe(PROSE_STEPS);
  });

  it("does not move in step with the face", () => {
    // Both come from one hash. If they took the same slice, every Oswald headline would sit
    // over the same size of prose — a pattern, which is the thing this is trying not to be.
    const ids = Array.from({ length: 600 }, (_, i) => "a_" + i);
    const pairs = new Set(
      assignVoices(ids.map((id) => ({ id }))).map(
        ({ article, voice }) => voice + ":" + proseStep(article.id),
      ),
    );
    expect(pairs.size).toBe(VOICES.length * PROSE_STEPS);
  });
});

describe("frameFor", () => {
  it("is the same every time, so a box does not come and go", () => {
    const id = "a_06G30HM6D10P236HGJDFTSHXFW";
    expect(frameFor(id)).toBe(frameFor(id));
  });

  it("boxes a minority, and spreads the three lines evenly among them", () => {
    const frames = Array.from({ length: 3000 }, (_, i) => frameFor("a_" + i));
    const tally = new Map<Frame, number>();
    for (const f of frames) tally.set(f, (tally.get(f) ?? 0) + 1);

    // Boxing is punctuation. A page where half the cards are boxed is a table.
    const boxed = frames.length - (tally.get("transparent") ?? 0);
    expect(boxed / frames.length).toBeGreaterThan(0.1);
    expect(boxed / frames.length).toBeLessThan(0.25);

    // And no line is rare enough to look like an accident when it turns up.
    for (const line of ["solid", "dashed", "dotted"] as const) {
      expect(tally.get(line) ?? 0).toBeGreaterThan(boxed / 6);
    }
  });
});
