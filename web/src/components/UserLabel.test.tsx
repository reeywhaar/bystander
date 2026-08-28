import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { UserLabel } from "@app/components/UserLabel";

describe("UserLabel", () => {
  it("says the name", () => {
    render(<UserLabel username="alice" />);
    expect(screen.getByText("alice")).toBeInTheDocument();
  });

  /*
   * The property the whole component exists for.
   *
   * Laid out as a flex row this looks right alone and sits wrong in company: a flex container
   * takes its baseline from its first item, an SVG has none, so the label is aligned by the
   * bottom of the icon while the links beside it are aligned by their type. That is a few
   * pixels of drift nobody can name and everybody can see, and it is exactly what the masthead
   * had. Inline, the text carries the baseline.
   */
  it("stays inline, so the text carries the baseline", () => {
    const { container } = render(<UserLabel username="alice" />);
    const label = container.firstElementChild;

    expect(label?.tagName).toBe("SPAN");
    expect(label?.className).not.toContain("flex");

    // The icon is placed against that baseline like any other inline object, rather than
    // being a flex item that decides it.
    const icon = container.querySelector("svg");
    expect(icon?.getAttribute("class")).toContain("inline");
    expect(icon?.getAttribute("class")).toContain("align-[-0.125em]");
  });

  // Decoration: the name is right there, so a label on the mark would be read out twice.
  it("leaves the mark out of the reading", () => {
    const { container } = render(<UserLabel username="alice" />);
    expect(container.querySelector("svg")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
  });
});
