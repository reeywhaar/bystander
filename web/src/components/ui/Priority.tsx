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
}: {
  value: number;
  onChange: (value: number) => void;
  label: string;
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
          className={`inline-block w-32 ${priority === 0 ? "text-accent" : ""}`}
        >
          {priority} · {describePriority(priority)}
        </span>
      )}
    />
  );
}
