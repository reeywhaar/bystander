import { describe, expect, it } from "vitest";

import { since, until } from "@app/lib/time";

// A fixed "now" in seconds, so these assert the arithmetic rather than the clock.
const NOW = 1_787_000_000;

describe("since", () => {
  it("counts up through the units", () => {
    expect(since(NOW - 10, NOW)).toBe("just now");
    expect(since(NOW - 60, NOW)).toBe("1 minute ago");
    expect(since(NOW - 180, NOW)).toBe("3 minutes ago");
    expect(since(NOW - 3600, NOW)).toBe("1 hour ago");
    expect(since(NOW - 7200, NOW)).toBe("2 hours ago");
    expect(since(NOW - 90_000, NOW)).toBe("yesterday");
    expect(since(NOW - 3 * 86_400, NOW)).toBe("3 days ago");
  });

  // A feed that publishes with a clock a little ahead of ours must not read as "-2 hours".
  it("does not go negative for a date in the future", () => {
    expect(since(NOW + 7200, NOW)).toBe("just now");
  });

  it("gives a date once a relative time stops being useful", () => {
    expect(since(NOW - 30 * 86_400, NOW)).toMatch(/\d/);
  });
});

describe("until", () => {
  it("counts down", () => {
    expect(until(NOW + 1800, NOW)).toBe("in 30 minutes");
    expect(until(NOW + 7200, NOW)).toBe("in 2 hours");
    expect(until(NOW + 2 * 86_400, NOW)).toBe("in 2 days");
  });

  // A page overdue by a minute is about to be made, not "in -1 minutes".
  it("says a moment that has passed is imminent", () => {
    expect(until(NOW - 60, NOW)).toBe("any moment");
    expect(until(NOW, NOW)).toBe("any moment");
  });
});
