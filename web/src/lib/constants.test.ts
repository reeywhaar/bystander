import { describe, expect, it } from "vitest";

import {
  lessOften,
  moreOften,
  PRIORITY_LADDER,
  DEFAULT_PRIORITY,
} from "@app/lib/constants";

describe("the priority ladder", () => {
  /*
   * The rule the rungs were chosen for: twenty either side of the middle, then fifteen, then
   * ten, then five into each end. Coarse where a feed is one of the crowd and fine where it is
   * nearly silent or nearly everything, which is where five actually means something — a share
   * is what this number buys, so at 5 the next rung doubles a feed's presence and at 50 it
   * moves it by two fifths.
   */
  it("steps less the further it is from the middle", () => {
    const steps = PRIORITY_LADDER.slice(1).map(
      (rung, i) => rung - PRIORITY_LADDER[i]!,
    );
    expect(steps).toEqual([5, 10, 15, 20, 20, 15, 10, 5]);
  });

  // Both ends are rungs rather than clamps: zero is reachable by pressing, and means never.
  it("runs from never to always, on the slider's own steps", () => {
    expect(PRIORITY_LADDER[0]).toBe(0);
    expect(PRIORITY_LADDER.at(-1)).toBe(100);
    expect(PRIORITY_LADDER).toContain(DEFAULT_PRIORITY);
    for (const rung of PRIORITY_LADDER) expect(rung % 5).toBe(0);
  });

  /*
   * The property a step computed from the value cannot have.
   *
   * From 50 a step proportional to the value lands on 30, and stepping back up from there
   * lands on 58. Pressing the wrong button and then the other one has to put somebody back
   * where they were, and shared rungs are the only way to get that exactly.
   */
  it("is exactly reversible at every rung", () => {
    for (const rung of PRIORITY_LADDER) {
      if (rung < 100) expect(lessOften(moreOften(rung))).toBe(rung);
      if (rung > 0) expect(moreOften(lessOften(rung))).toBe(rung);
    }
  });

  it("stops at each end rather than running past it", () => {
    expect(lessOften(0)).toBe(0);
    expect(moreOften(100)).toBe(100);
  });

  // The slider steps by five, so it can leave a feed between rungs. A press moves to the
  // neighbouring rung rather than adding to a number that is not on the ladder.
  it("snaps a value the slider left between rungs", () => {
    expect(moreOften(45)).toBe(50);
    expect(lessOften(45)).toBe(30);
    expect(moreOften(10)).toBe(15);
    expect(lessOften(10)).toBe(5);
  });
});
