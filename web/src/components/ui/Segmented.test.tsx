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

  // The tint slides rather than each segment lighting up where it stands, so the eye follows
  // the answer across instead of finding it again. Its position is arithmetic against the
  // content box — the browser would resolve a bare percentage against the padding box, which
  // is a couple of pixels adrift at every stop.
  it("puts the slider over the answer, measured inside the padding", () => {
    const { rerender, container } = render(
      <Segmented label="How" options={OPTIONS} value={0} onChange={() => {}} />,
    );

    const slider = () => container.querySelector<HTMLElement>("[aria-hidden]");
    expect(slider()).toHaveStyle({
      width: "calc((100% - 0.5rem) / 2)",
      left: "calc(0.25rem + (100% - 0.5rem) * 0 / 2)",
    });

    rerender(
      <Segmented label="How" options={OPTIONS} value={1} onChange={() => {}} />,
    );
    expect(slider()).toHaveStyle({
      left: "calc(0.25rem + (100% - 0.5rem) * 1 / 2)",
    });
  });

  it("has no slider when there is no answer", () => {
    const { container } = render(
      <Segmented
        label="How"
        options={OPTIONS}
        value={null}
        onChange={() => {}}
      />,
    );
    expect(container.querySelector("[aria-hidden]")).toBeNull();
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
