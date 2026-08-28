import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Logo } from "@app/components/Logo";

/**
 * The logo exists twice, and these are the properties that make the copy worth having.
 *
 * logo.svg at the repository root is the file the README and anything outside this app use.
 * Logo.tsx is the same drawing inlined, which is what lets it take `currentColor` — invert on
 * the dark page, answer a hover — none of which an `<img>` can do without a second file and a
 * media query of its own.
 *
 * There is deliberately no test comparing the two. Reading the root file would mean either
 * `@types/node`, which this project does not have and says so in vite.config.ts, or widening
 * the dev server's `fs.allow` past its own root for the sake of an assertion. Both are a bigger
 * change to the build than the risk they cover. The two are kept in step by hand; if they drift
 * the masthead stops matching the README, which is visible rather than silent.
 */
describe("Logo", () => {
  // The lockup is four shapes: three for the head and its two echoes, one for the word. A
  // count is a weak check by itself, and it is here because it fails loudly if half the
  // drawing goes missing in an edit.
  it("draws the whole lockup", () => {
    const { container } = render(<Logo />);
    expect(container.querySelectorAll("path")).toHaveLength(4);
  });

  // Cropped to the ink, with no built-in margin: whatever it is set into gives it its space.
  // A box that grew padding would push every masthead off its own alignment.
  it("is cropped to its own ink", () => {
    const { container } = render(<Logo />);
    expect(container.querySelector("svg")?.getAttribute("viewBox")).toBe(
      "0.10 -96.50 625.05 120.70",
    );
  });

  /*
   * The one thing the inlined copy is *for*. Every path takes its colour from the type around
   * it, so the lockup inverts with the page instead of needing a light file and a dark one.
   */
  it("takes its colour from whatever it is set in", () => {
    const { container } = render(<Logo />);
    const svg = container.querySelector("svg");

    expect(svg?.getAttribute("fill")).toBe("currentColor");
    for (const path of container.querySelectorAll("path")) {
      const own = path.getAttribute("fill");
      expect(own === null || own === "currentColor").toBe(true);
    }
  });

  it("is announced as the name rather than as a picture", () => {
    const { container } = render(<Logo />);
    const svg = container.querySelector("svg");

    expect(svg?.getAttribute("role")).toBe("img");
    expect(svg?.getAttribute("aria-label")).toBe("the bystander");
  });
});
