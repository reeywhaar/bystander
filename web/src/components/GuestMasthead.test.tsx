import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GuestMasthead } from "@app/components/GuestMasthead";

describe("GuestMasthead", () => {
  it("offers the source only where it is asked for", () => {
    const { rerender } = render(<GuestMasthead onSignIn={() => {}} />);
    expect(screen.queryByRole("link", { name: "GitHub" })).toBeNull();

    rerender(<GuestMasthead onSignIn={() => {}} source />);
    expect(screen.getByRole("link", { name: "GitHub" })).toBeInTheDocument();
  });

  /*
   * The same property UserLabel exists for, in the one other place a mark sits beside a word.
   *
   * The links are a row aligned by baseline. Laid out as a flex row this link looks right on
   * its own and sits wrong in company: a flex container takes its baseline from its first
   * item, an SVG has none, so one is synthesised from the bottom of the mark and the word
   * "GitHub" drops below "Sign in" beside it. Measured at 14px type that was seven pixels.
   * Inline, the word carries the baseline.
   */
  it("keeps the mark inline, so the word carries the baseline", () => {
    render(<GuestMasthead onSignIn={() => {}} source />);
    const link = screen.getByRole("link", { name: "GitHub" });

    expect(link.className).not.toContain("flex");

    const mark = link.querySelector("svg");
    expect(mark?.getAttribute("class")).toContain("inline");
    expect(mark?.getAttribute("class")).toContain("align-[-0.125em]");
  });
});
