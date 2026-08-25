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
 * A `radiogroup` rather than buttons that happen to look chosen: it is one question with one
 * answer, and that is what a screen reader should be told. The pressed segment is tinted the
 * same way the tab strips are — two controls meaning "you are on this one" should not be two
 * different objects.
 */
export function Segmented({
  options,
  value,
  onChange,
  label,
  disabled = false,
}: {
  options: string[];
  /** Which option is chosen, by position, or null for none of them. */
  value: number | null;
  onChange: (index: number) => void;
  /** What the question is, read out before the answers. */
  label: string;
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
        className="inline-flex w-fit gap-1 rounded-md border border-rule bg-paper-sunken p-1"
      >
        {options.map((option, index) => (
          <button
            key={option}
            type="button"
            role="radio"
            aria-checked={index === at}
            disabled={disabled}
            onClick={() => onChange(index)}
            className={`rounded px-3 py-1.5 text-sm whitespace-nowrap disabled:opacity-50 ${
              index === at
                ? "bg-accent/10 text-accent"
                : "text-ink-muted hover:text-ink"
            }`}
          >
            {option}
          </button>
        ))}
      </div>
    </div>
  );
}
