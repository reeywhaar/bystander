import { describe, expect, it } from "vitest";

import {
  assignVoices,
  columnsFor,
  FRAME_RANKS,
  LINES,
  MAX_COLUMNS,
  PROSE_STEPS,
  styleFor,
  VOICES,
  type Style,
} from "@app/lib/voice";

/** Ids in the shape ids.go mints them: a prefix and a tail that varies late. */
const ids = (count: number) =>
  Array.from({ length: count }, (_, i) => "a_06G30HM6D10P236HGJDFTSH" + i);

const EDITION = "e_06G30HM6D10P236HGJDFTSHXFW";

const styles = (count: number, edition = EDITION) =>
  ids(count).map((id) => styleFor(edition, id));

describe("styleFor", () => {
  it("deals the same hand every time, so nothing moves under a reader", () => {
    const a = styleFor(EDITION, "a_1");
    const b = styleFor(EDITION, "a_1");
    expect(a).toEqual(b);
  });

  // The page is fixed until the next one is composed. An article that survives into it is on
  // a different page, and a different page should look like one.
  it("deals a different hand on the next page", () => {
    const today = ids(200).map((id) => styleFor(EDITION, id));
    const tomorrow = ids(200).map((id) => styleFor("e_TOMORROW", id));

    const same = today.filter(
      (s, i) => JSON.stringify(s) === JSON.stringify(tomorrow[i]),
    );
    // A few will coincide; nearly all of them coinciding would mean the edition is not in
    // the seed at all.
    expect(same.length).toBeLessThan(today.length / 4);
  });

  it("uses the whole of every ladder", () => {
    const drawn = styles(3000);

    expect(new Set(drawn.map((s) => s.voice)).size).toBe(VOICES.length);
    expect(new Set(drawn.map((s) => s.prose)).size).toBe(PROSE_STEPS);

    const boxed = drawn.map((s) => s.frame).filter((f) => f !== null);
    expect(new Set(boxed.map((f) => f.line)).size).toBe(LINES.length);
    expect(new Set(boxed.map((f) => f.width)).size).toBe(FRAME_RANKS);
    expect(new Set(boxed.map((f) => f.ink)).size).toBe(FRAME_RANKS);
    expect(new Set(boxed.map((f) => f.pad)).size).toBe(FRAME_RANKS);
  });

  it("leaves at least half the cards unboxed", () => {
    const drawn = styles(3000);
    const boxed = drawn.filter((s) => s.frame !== null);

    // One in five, which is what the bag says. Loose bounds on purpose: this is here to
    // catch the bag being edited into a page of boxes or a page of none, not to hold a ratio
    // to the third decimal.
    expect(boxed.length / drawn.length).toBeGreaterThan(0.1);
    expect(boxed.length / drawn.length).toBeLessThan(0.3);

    // Dashed is the commonest line, by the same bag.
    const dashed = boxed.filter((s) => s.frame!.line === "dashed").length;
    expect(dashed).toBeGreaterThan(boxed.length / 4);

    // Null rather than an invisible border: the padding belongs to the box, and an unboxed
    // card should not carry an inset it has no use for.
    expect(drawn.some((s) => s.frame === null)).toBe(true);
  });

  it("draws each of a frame's four marks separately", () => {
    const boxed = styles(6000)
      .map((s) => s.frame)
      .filter((f) => f !== null);

    // Every combination reachable. If two of these moved together, a box would be one mark
    // repeated rather than one that differs from the last.
    const seen = new Set(
      boxed.map((f) => `${f.line}:${f.width}:${f.ink}:${f.pad}`),
    );
    expect(seen.size).toBe(LINES.length * FRAME_RANKS ** 3);
  });

  it("does not move the face and the size in step", () => {
    // Both come off one stream. Successive draws are independent, and the test that says so
    // is that every pairing turns up.
    const drawn = styles(6000);
    const pairs = new Set(drawn.map((s) => `${s.voice}:${s.prose}`));
    expect(pairs.size).toBe(VOICES.length * PROSE_STEPS);
  });

  it("rules the page into bands, rarely and at irregular intervals", () => {
    const drawn = styles(6000);
    const ruled = drawn.filter((s) => s.rule).length;

    // One or two on a ninety-article page. It was ten per cent to begin with, which put a
    // rule every ten cards — often enough that it stops being a mark anybody sees, and a
    // mark nobody sees is not dividing anything.
    const perPage = (ruled / drawn.length) * 90;
    expect(perPage).toBeGreaterThan(0.5);
    expect(perPage).toBeLessThan(3);

    // Irregular, which is the whole reason this is a chance and not a cadence. Even spacing
    // reads as a table's gridlines.
    const gaps: number[] = [];
    let last = -1;
    drawn.forEach((style, i) => {
      if (!style.rule) return;
      if (last >= 0) gaps.push(i - last);
      last = i;
    });
    expect(new Set(gaps).size).toBeGreaterThan(5);
  });

  it("needs enough text before it splits a body into columns", () => {
    // Three lines cut down the middle is one paragraph with a gap in it.
    const short = ids(400).map((id) => styleFor(EDITION, id, "Too little."));
    expect(short.every((s) => s.columns === 1)).toBe(true);

    const long = ids(400).map((id) => styleFor(EDITION, id, "x".repeat(900)));
    expect(long.some((s) => s.columns > 1)).toBe(true);
    // And not all of them, or a page would have no single-column wide stories to contrast.
    expect(long.some((s) => s.columns === 1)).toBe(true);
  });

  it("draws every column count, and none above the ceiling", () => {
    const long = ids(3000).map((id) => styleFor(EDITION, id, "x".repeat(900)));
    const counts = new Set(long.map((s) => s.columns));

    for (let n = 1; n <= MAX_COLUMNS; n++) {
      expect(counts.has(n)).toBe(true);
    }
    expect(Math.max(...counts)).toBe(MAX_COLUMNS);
    expect(Math.min(...counts)).toBe(1);

    // Half of them unsplit, so a split stays the exception. Loose bounds: this is here to
    // catch a page of nothing but columns, not to hold a ratio.
    const single = long.filter((s) => s.columns === 1).length;
    expect(single / long.length).toBeGreaterThan(0.3);
    expect(single / long.length).toBeLessThan(0.7);
  });

  it("sets a picture beside the story on some cards and not others", () => {
    const drawn = styles(2000);
    const beside = drawn.filter((s) => s.aside).length;

    // A coin toss, and it only means anything on a boxed card — which is one in five, so
    // this is about one card in ten on a real page.
    expect(beside / drawn.length).toBeGreaterThan(0.4);
    expect(beside / drawn.length).toBeLessThan(0.6);
  });
});

describe("columnsFor", () => {
  // The drawn number is a preference; the slot is the constraint. A quarter-page card set in
  // four columns is four ribbons, whatever it drew.
  it("holds a card to what its width can carry", () => {
    expect(columnsFor("lead", 4)).toBe(4);
    expect(columnsFor("wide", 4)).toBe(3);
    expect(columnsFor("feature", 4)).toBe(2);
    expect(columnsFor("standard", 4)).toBe(1);
    expect(columnsFor("brief", 4)).toBe(1);
  });

  it("never widens a card that asked for less", () => {
    expect(columnsFor("lead", 1)).toBe(1);
    expect(columnsFor("lead", 2)).toBe(2);
    expect(columnsFor("wide", 2)).toBe(2);
  });

  // A slot nobody has heard of is a slot this page cannot reason about, and one column is the
  // answer that is never wrong.
  it("gives an unknown slot a single column", () => {
    expect(columnsFor("something-new", 4)).toBe(1);
  });
});

describe("assignVoices", () => {
  it("never sets two headlines in a row in the same face", () => {
    // The one rule a newspaper's headline typography actually has. An independent draw
    // breaks it about one time in six, which reads as the page having failed to notice.
    const voices = assignVoices(styles(500));
    for (let i = 1; i < voices.length; i++) {
      expect(voices[i]).not.toBe(voices[i - 1]);
    }
  });

  it("keeps using every face while it fixes them up", () => {
    const voices = assignVoices(styles(500));
    expect(new Set(voices).size).toBe(VOICES.length);
  });

  it("does not turn every collision into the same pair", () => {
    // A fixed step would make didone always follow antique, which is a pattern a reader
    // notices well before they could name it.
    const drawn = styles(2000);
    const voices = assignVoices(drawn);

    const after = new Map<string, Set<string>>();
    for (let i = 1; i < voices.length; i++) {
      if (drawn[i]!.voice !== voices[i - 1]) continue; // not a collision
      const from = voices[i - 1]!;
      if (!after.has(from)) after.set(from, new Set());
      after.get(from)!.add(voices[i]!);
    }
    for (const [, landed] of after) {
      expect(landed.size).toBeGreaterThan(1);
    }
  });

  it("leaves a page whose faces already differ exactly as it found it", () => {
    const drawn: Style[] = VOICES.map((voice) => ({
      voice,
      prose: 0,
      frame: null,
      columns: 1,
      rule: false,
      shot: 2,
      fit: "cover",
      aside: false,
    }));
    expect(assignVoices(drawn)).toEqual([...VOICES]);
  });
});
