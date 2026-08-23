import { useEffect, useState, type ReactNode } from "react";

/**
 * A range input that follows the cursor and saves when you let go.
 *
 * Both of the obvious ways to write this are wrong, and it had both:
 *
 *  - Driving `value` from the server means the thumb cannot move until a round trip
 *    finishes. It travels one step, snaps back, and waits.
 *  - Disabling while that request is in flight makes it worse: the input goes dead
 *    mid-drag, so the browser abandons the gesture and the pointer leaves the thumb
 *    behind.
 *
 * So the position is local state while a finger is on it, and the change is committed on
 * release — `pointerup` for a mouse or a touch, `keyup` for the arrow keys, `blur` for
 * anything that takes focus away mid-drag. That also means one request per gesture rather
 * than one per pixel.
 */
export function Slider({
  value,
  min,
  max,
  step = 1,
  onCommit,
  label,
  format,
  className = "w-64",
}: {
  value: number;
  min: number;
  max: number;
  step?: number;
  onCommit: (value: number) => void;
  label: string;
  /** Renders the number beside the track, from the position under the finger. */
  format: (value: number) => ReactNode;
  className?: string;
}) {
  const [local, setLocal] = useState(value);

  // Follow the stored value when it changes from somewhere else. Nothing is in flight
  // during a drag — the commit only happens on release — so this cannot fight the thumb.
  useEffect(() => setLocal(value), [value]);

  function commit() {
    if (local !== value) onCommit(local);
  }

  return (
    <span className="flex items-center gap-3 text-xs text-ink-muted">
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={local}
        onChange={(event) => setLocal(Number(event.target.value))}
        onPointerUp={commit}
        onKeyUp={commit}
        onBlur={commit}
        aria-label={label}
        className={`${className} accent-[var(--accent)]`}
      />
      <span className="tabular-nums">{format(local)}</span>
    </span>
  );
}
