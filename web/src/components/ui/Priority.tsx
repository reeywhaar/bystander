import { describePriority } from "@app/lib/constants";

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
  disabled,
  label,
}: {
  value: number;
  onChange: (value: number) => void;
  disabled?: boolean;
  label: string;
}) {
  return (
    <label className="flex items-center gap-3 text-xs text-ink-muted">
      <span className="sr-only">{label}</span>
      <input
        type="range"
        min={0}
        max={100}
        step={5}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(Number(event.target.value))}
        className="w-32 accent-[var(--accent)]"
        aria-label={label}
      />
      <span className={`w-24 tabular-nums ${value === 0 ? "text-accent" : ""}`}>
        {value} · {describePriority(value)}
      </span>
    </label>
  );
}
