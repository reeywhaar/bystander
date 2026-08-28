import type { ReactNode } from "react";

import { describePriority } from "@app/lib/constants";
import { Slider } from "@app/components/ui/Slider";

/**
 * A priority, 0..100.
 *
 * Shown with words beside the number because the number alone does not say what it does.
 * "50" is meaningless; "as usual" is the whole model in two words — this is a probability
 * of being drawn, not a position in an ordering.
 */
export function Priority({
  value,
  onChange,
  label,
  leading,
}: {
  value: number;
  onChange: (value: number) => void;
  label: string;
  /**
   * Something to sit immediately before the value, inside the space the label reserves.
   *
   * For a control that belongs with this one but is not part of it — the feed list's
   * preview, which is the row's other thing-you-do-to-a-feed. Put outside the Priority
   * instead, it lands at the far edge of a fixed-width label box that is right-aligned
   * against the track, and a 16px icon alone in seventy pixels of white reads as a mistake
   * rather than as a button.
   *
   * The value goes left-aligned when there is one, so the two are a pair rather than two
   * things at opposite ends of the same box. The track still cannot move: the box is the
   * same width whatever is in it, which is the whole reason it has one.
   */
  leading?: ReactNode;
}) {
  return (
    <Slider
      value={value}
      min={0}
      max={100}
      step={5}
      onCommit={onChange}
      label={label}
      className="w-32"
      // Wide enough for the longest of these — "100 · more often" — so the label occupies
      // the same space at every value and nothing moves while the thumb does.
      format={(priority) => (
        <span
          className={`inline-flex w-32 items-center gap-2 text-left ${
            leading ? "" : "sm:justify-end"
          } ${priority === 0 ? "text-accent" : ""}`}
        >
          {leading}
          <span>
            {priority} · {describePriority(priority)}
          </span>
        </span>
      )}
    />
  );
}
