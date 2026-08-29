import { useEffect, useState, type CSSProperties, type ReactNode } from "react";

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
  stacked = false,
  fill = false,
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
  /**
   * Track on its own full-width line with the value beneath it, right-aligned.
   *
   * For a control that owns its row rather than sitting at the end of one. Inline, the
   * value has to hold a fixed width so a longer word cannot squeeze the track; stacked,
   * nothing is competing for the space, so it can simply sit under the end of the track it
   * describes.
   */
  stacked?: boolean;
  /**
   * Value on its own line, and a track filling the width beneath it.
   *
   * The inverse of [stacked], which puts the track first and the value under its right-hand
   * end. Both exist because they answer different questions: stacked is a control at the end
   * of a form, where the number is a result; this one is a *field*, sitting in a box beside
   * other fields, where the number reads as its label.
   */
  fill?: boolean;
}) {
  const [local, setLocal] = useState(value);

  // Follow the stored value when it changes from somewhere else. Nothing is in flight
  // during a drag — the commit only happens on release — so this cannot fight the thumb.
  useEffect(() => setLocal(value), [value]);

  function commit() {
    if (local !== value) onCommit(local);
  }

  const track = (
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
      // shrink-0 is load-bearing. The label beside the track changes length as the value
      // moves — "as usual" becomes "less often" — and a flex item will not shrink below
      // its own content, so the growing label was squeezing the track under the finger
      // that was dragging it.
      className={`slider ${stacked || fill ? "w-full" : className} shrink-0`}
      // How much of the track is behind the thumb, for the gradient that draws the fill —
      // see `.slider` in styles.css. From the position under the finger rather than from the
      // stored value, so the fill follows the drag instead of catching up on release.
      style={
        {
          "--slider-fill": `${((local - min) / (max - min)) * 100}%`,
        } as CSSProperties
      }
    />
  );

  if (fill) {
    return (
      <span className="flex w-full flex-col gap-1.5 text-xs text-ink-muted">
        <span className="tabular-nums">{format(local)}</span>
        {track}
      </span>
    );
  }

  if (stacked) {
    return (
      <span className="flex w-full flex-col gap-1.5">
        {track}
        <span className="text-right text-xs tabular-nums text-ink-muted">
          {format(local)}
        </span>
      </span>
    );
  }

  return (
    // Inline, the value reads before the track rather than after: it is what the control
    // is *for*, and a number chasing along behind the thumb is harder to read than one
    // holding still in front of it.
    <span className="flex items-center gap-3 text-xs text-ink-muted">
      <span className="shrink-0 whitespace-nowrap tabular-nums">
        {format(local)}
      </span>
      {track}
    </span>
  );
}
