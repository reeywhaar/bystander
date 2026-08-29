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
  fill = false,
}: {
  value: number;
  onChange: (value: number) => void;
  label: string;
  /**
   * Two lines: the value, then a track filling the width under it.
   *
   * For the places this sits in a box of its own beside other fields, where it has a width
   * given to it and should use all of it — a 128px track in the middle of a wider box is a
   * control somebody has to aim at, and the value floating beside it reads as a caption
   * rather than as the field's own label.
   */
  fill?: boolean;
}) {
  return (
    <Slider
      value={value}
      min={0}
      max={100}
      step={5}
      onCommit={onChange}
      label={label}
      fill={fill}
      className="w-32"
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
          className={`inline-flex items-center gap-2 ${
            fill ? "w-full" : "w-32 justify-end"
          } ${priority === 0 ? "text-accent" : ""}`}
        >
          <span>
            {priority} · {describePriority(priority)}
          </span>
        </span>
      )}
    />
  );
}
