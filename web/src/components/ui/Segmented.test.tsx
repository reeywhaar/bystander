import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Segmented } from "@app/components/ui/Segmented";

const OPTIONS = ["Link", "Email"];

describe("Segmented", () => {
  it("is one question with one answer", async () => {
    const onChange = vi.fn();
    render(
      <Segmented
        label="How it reaches them"
        options={OPTIONS}
        value={0}
        onChange={onChange}
      />,
    );

    const group = screen.getByRole("radiogroup", {
      name: "How it reaches them",
    });
    expect(group).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Link" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Email" })).not.toBeChecked();

    await userEvent.click(screen.getByRole("radio", { name: "Email" }));
    expect(onChange).toHaveBeenCalledWith(1);
  });

  // A question that has not been put yet.
  it("lights nothing when there is no answer", () => {
    render(
      <Segmented
        label="How"
        options={OPTIONS}
        value={null}
        onChange={() => {}}
      />,
    );
    for (const option of OPTIONS) {
      expect(screen.getByRole("radio", { name: option })).not.toBeChecked();
    }
  });

  // Options can shrink under a value that was valid when it was set. Showing the nearest
  // beats showing no answer to a question that has one.
  it("clamps an index past the end", () => {
    render(
      <Segmented label="How" options={OPTIONS} value={7} onChange={() => {}} />,
    );
    expect(screen.getByRole("radio", { name: "Email" })).toBeChecked();
  });

  it("clamps a negative index", () => {
    render(
      <Segmented
        label="How"
        options={OPTIONS}
        value={-3}
        onChange={() => {}}
      />,
    );
    expect(screen.getByRole("radio", { name: "Link" })).toBeChecked();
  });
});
