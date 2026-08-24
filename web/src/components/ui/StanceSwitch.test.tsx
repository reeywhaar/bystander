import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import {
  StanceSwitch,
  stanceOf,
  withStance,
  type Stance,
} from "@app/components/ui/StanceSwitch";

const SAYS: Record<Stance, string> = {
  exclude: "never",
  neutral: "no opinion",
  include: "always",
};

function Harness({ start = "neutral" }: { start?: Stance }) {
  const [value, setValue] = useState<Stance>(start);
  return (
    <>
      <StanceSwitch
        value={value}
        onChange={setValue}
        name="Finance"
        says={SAYS}
      />
      <output>{value}</output>
    </>
  );
}

describe("StanceSwitch", () => {
  it("takes the answer that was pressed rather than cycling", () => {
    render(<Harness />);
    // Pressing where you want it. A control that cycles makes somebody pass through an answer
    // they did not want, which on a filter means a page briefly drawing from the wrong things.
    fireEvent.click(screen.getByLabelText("Finance: always"));
    expect(screen.getByRole("status", { hidden: true }).textContent).toBe(
      "include",
    );
    fireEvent.click(screen.getByLabelText("Finance: never"));
    expect(screen.getByRole("status", { hidden: true }).textContent).toBe(
      "exclude",
    );
  });

  it("says which answer is chosen, so it can be read without seeing it", () => {
    render(<Harness start="include" />);
    expect(screen.getByLabelText("Finance: always")).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByLabelText("Finance: never")).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });
});

describe("withStance", () => {
  // The whole reason this is one control rather than two lists of ticks: a name on both sides
  // is a contradiction the server refuses, and a control that cannot express it beats an error
  // explaining it.
  it("never leaves a name on both sides", () => {
    let sides = { include: ["a"], exclude: ["b"] };

    sides = withStance("a", "exclude", sides.include, sides.exclude);
    expect(sides.include).not.toContain("a");
    expect(sides.exclude).toContain("a");

    sides = withStance("a", "include", sides.include, sides.exclude);
    expect(sides.exclude).not.toContain("a");
    expect(sides.include).toContain("a");
  });

  it("takes a name off both sides when it goes back to neutral", () => {
    const sides = withStance("a", "neutral", ["a"], []);
    expect(sides.include).toEqual([]);
    expect(sides.exclude).toEqual([]);
    expect(stanceOf("a", sides.include, sides.exclude)).toBe("neutral");
  });

  it("leaves everything else alone", () => {
    const sides = withStance("b", "include", ["a"], ["c"]);
    expect(sides.include).toContain("a");
    expect(sides.exclude).toEqual(["c"]);
  });
});
