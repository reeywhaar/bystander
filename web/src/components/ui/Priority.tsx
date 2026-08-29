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
   * It rides with the value against the track rather than holding a position of its own, so
   * the pair is tight and whatever slack the box has falls to the left of both, where there
   * is nothing to notice it. The cost is that it shifts a little as the value's wording
   * changes — but the only time that happens is while somebody is dragging the thumb, which
   * is the one moment they are not reaching for it. The track itself still cannot move: the
   * box is the same width whatever is in it, which is the whole reason it has one.
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
      // Full width on a phone, where this owns its line and a 128px track in the middle of
      // it is a control somebody has to aim at. Both halves widen together, so the value
      // takes a line of its own and the track sits under it — see the wrapping row in Slider.
      className="w-full sm:w-32"
      // Wide enough for the longest of these — "100 · more often" — so the label occupies
      // the same space at every value and nothing moves while the thumb does.
      //
      // And right-aligned inside that box at every width, so the words sit against the track
      // they describe. It used to be left-aligned below `sm`, which put the box's slack
      // *between* the two halves — a phone showed "50 · as usual", a gap, and then a slider,
      // and the pair stopped reading as one control. Anchored to the track, the slack falls
      // outside the whole thing, where the layout around it can decide which end it goes to.
      format={(priority) => (
        <span
          className={`inline-flex w-full items-center justify-end gap-2 sm:w-32 ${
            priority === 0 ? "text-accent" : ""
          }`}
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
