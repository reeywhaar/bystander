import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Slider } from "@app/components/ui/Slider";

function renderSlider(onCommit = vi.fn(), value = 50) {
  render(
    <Slider
      value={value}
      min={0}
      max={100}
      step={10}
      onCommit={onCommit}
      label="How many"
      format={(v) => `${v} articles`}
    />,
  );
  return { input: screen.getByLabelText("How many"), onCommit };
}

describe("Slider", () => {
  // The bug this exists to prevent: the thumb moved one step and snapped back, because the
  // position came from the server and the control was disabled while the save was in
  // flight.
  it("follows the cursor without waiting for anything", () => {
    const { input } = renderSlider();

    for (const step of ["60", "70", "80"]) {
      fireEvent.change(input, { target: { value: step } });
      expect(input).toHaveValue(step);
      expect(screen.getByText(`${step} articles`)).toBeInTheDocument();
    }
  });

  it("saves once, when the drag ends", () => {
    const { input, onCommit } = renderSlider();

    fireEvent.change(input, { target: { value: "60" } });
    fireEvent.change(input, { target: { value: "70" } });
    fireEvent.change(input, { target: { value: "80" } });
    // One request per gesture, not one per pixel.
    expect(onCommit).not.toHaveBeenCalled();

    fireEvent.pointerUp(input);
    expect(onCommit).toHaveBeenCalledTimes(1);
    expect(onCommit).toHaveBeenCalledWith(80);
  });

  it("saves when the arrow keys are let go", () => {
    const { input, onCommit } = renderSlider();

    fireEvent.change(input, { target: { value: "60" } });
    fireEvent.keyUp(input, { key: "ArrowRight" });
    expect(onCommit).toHaveBeenCalledWith(60);
  });

  // A drag interrupted by something taking focus should still be saved rather than lost.
  it("saves when focus leaves mid-drag", () => {
    const { input, onCommit } = renderSlider();

    fireEvent.change(input, { target: { value: "30" } });
    fireEvent.blur(input);
    expect(onCommit).toHaveBeenCalledWith(30);
  });

  it("says nothing when the value did not move", () => {
    const { input, onCommit } = renderSlider();

    fireEvent.pointerUp(input);
    fireEvent.blur(input);
    expect(onCommit).not.toHaveBeenCalled();
  });

  it("takes a value changed from somewhere else", () => {
    const onCommit = vi.fn();
    const { rerender } = render(
      <Slider
        value={50}
        min={0}
        max={100}
        step={10}
        onCommit={onCommit}
        label="How many"
        format={(v) => `${v} articles`}
      />,
    );
    rerender(
      <Slider
        value={90}
        min={0}
        max={100}
        step={10}
        onCommit={onCommit}
        label="How many"
        format={(v) => `${v} articles`}
      />,
    );

    expect(screen.getByLabelText("How many")).toHaveValue("90");
    expect(onCommit).not.toHaveBeenCalled();
  });
});
