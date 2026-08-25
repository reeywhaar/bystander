import type { ReactNode, SelectHTMLAttributes } from "react";
import { useId } from "react";

/**
 * A labelled select, shaped like [Field] and styled once.
 *
 * It exists because there were three of these, hand-styled in three components, and three
 * copies of a style are three chances for one of them to be a little different — which is how
 * a form ends up with two boxes that are nearly but not quite the same height.
 *
 * `label` may be omitted for a select that sits in a row of its own controls and is labelled
 * by what is around it; it becomes an `aria-label` instead, so the control is never nameless.
 *
 * **The padding is asymmetric on purpose.** A browser draws the arrow *inside* the padding
 * box rather than beside it, so even padding puts it hard against the border — cramped at one
 * end and nowhere else, which reads as a rendering fault rather than as a style.
 */
export function Select({
  label,
  hint,
  small = false,
  className = "",
  children,
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement> & {
  label?: string;
  hint?: ReactNode;
  /** For a select that sits inside a row rather than in a form. */
  small?: boolean;
  children: ReactNode;
}) {
  const id = useId();
  const hintId = `${id}-hint`;

  const box = small
    ? "py-1 pr-7 pl-2 text-xs text-ink-muted"
    : "py-2 pr-8 pl-3 text-sm text-ink";

  const select = (
    <select
      id={id}
      aria-label={label ? undefined : rest["aria-label"]}
      aria-describedby={hint ? hintId : undefined}
      className={`rounded-md border border-rule bg-paper-raised ${box}
        focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent
        ${label ? "" : className}`}
      {...rest}
    >
      {children}
    </select>
  );

  if (!label) return select;

  return (
    <div className={`flex flex-col gap-1.5 ${className}`}>
      <label htmlFor={id} className="text-sm font-medium text-ink">
        {label}
      </label>
      {hint ? (
        <p id={hintId} className="text-xs text-ink-muted">
          {hint}
        </p>
      ) : null}
      {select}
    </div>
  );
}
