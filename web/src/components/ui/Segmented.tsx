import { useId } from "react";

/**
 * One question with a few answers, laid out as a row of them.
 *
 * A segmented control rather than a `<select>` for a choice this short. A select hides its
 * options behind a press, which is right when there are ten and wasteful when there are two —
 * and where the answer changes what the rest of a form is, seeing both at once is most of what
 * makes the form legible.
 *
 * Options are plain strings and the answer is an index. The caller already has the meanings in
 * an array to render them from, so anything richer would be that array written twice.
 *
 * `null` is a real answer — nothing chosen — for a question that has not been put yet. An index
 * past the end is clamped rather than leaving nothing lit: options can shrink under a value
 * that was valid when it was set, and a control showing no answer to a question that has one
 * is a worse lie than one showing the nearest.
 *
 * `block` fills the width and divides it evenly between the answers, which is what a form
 * wants: sized to their own labels, one control as wide as "Ordinary | Administrator" above
 * another as wide as "Link | Email" reads as two unrelated things rather than two answers to
 * the same form. Off by default, because a control in a row of other controls should be its
 * own size — opted into, like every other switch on these primitives.
 *
 * A `radiogroup` rather than buttons that happen to look chosen: it is one question with one
 * answer, and that is what a screen reader should be told.
 */
export function Segmented({
  options,
  value,
  onChange,
  label,
  block = false,
  disabled = false,
}: {
  options: string[];
  /** Which option is chosen, by position, or null for none of them. */
  value: number | null;
  onChange: (index: number) => void;
  /** What the question is, read out before the answers. */
  label: string;
  /** Fill the width, dividing it evenly between the answers. */
  block?: boolean;
  disabled?: boolean;
}) {
  const id = useId();
  const at =
    value === null ? null : Math.min(Math.max(value, 0), options.length - 1);

  return (
    <div className="flex flex-col gap-1.5">
      <span id={id} className="text-sm font-medium text-ink">
        {label}
      </span>
      <div
        role="radiogroup"
        aria-labelledby={id}
        className={`relative rounded-md border border-rule bg-paper-sunken p-1 ${
          block ? "flex w-full" : "inline-flex w-fit"
        }`}
      >
        {/*
          The tint slides rather than each segment lighting up where it stands, so the eye
          follows the answer from the old one to the new instead of finding it again.

          Measured against the *content* box, not the percentage the browser would resolve.
          An absolutely positioned child is placed against its container's padding box, so a
          plain `left: 50%` is half of the track including its own padding — a couple of
          pixels adrift at every position, which is near enough to look like a mistake and not
          near enough to be one anybody could name. `p-1` is 0.25rem a side, so the track is
          `100% - 0.5rem` wide and starts 0.25rem in. See StanceSwitch, which learned this the
          same way.
        */}
        {at === null ? null : (
          <span
            aria-hidden
            className="pointer-events-none absolute top-1 bottom-1 rounded bg-accent/10
              transition-[left,width] duration-150 ease-out motion-reduce:transition-none"
            style={{
              width: `calc((100% - 0.5rem) / ${options.length})`,
              left: `calc(0.25rem + (100% - 0.5rem) * ${at} / ${options.length})`,
            }}
          />
        )}

        {options.map((option, index) => (
          <button
            key={option}
            type="button"
            role="radio"
            aria-checked={index === at}
            disabled={disabled}
            onClick={() => onChange(index)}
            // `basis-0 grow` in both modes, so every segment is the same width and the
            // arithmetic above holds. In the inline case that makes the track as wide as its
            // widest answer times the number of them, which is what stops a two-word option
            // dwarfing a one-word one.
            className={`relative basis-0 grow rounded px-3 py-1.5 text-sm whitespace-nowrap
              transition-colors duration-150 disabled:opacity-50 ${
                index === at ? "text-accent" : "text-ink-muted hover:text-ink"
              }`}
          >
            {option}
          </button>
        ))}
      </div>
    </div>
  );
}
