import type { InputHTMLAttributes, ReactNode } from "react";
import { useId } from "react";

/**
 * A labelled input.
 *
 * The label is bound by a generated id rather than by wrapping, so a hint can sit between
 * the two without ending up inside the label and being read out as part of it.
 */
export function Field({
  label,
  hint,
  error,
  className = "",
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  hint?: ReactNode;
  error?: string;
}) {
  const id = useId();
  const hintId = `${id}-hint`;

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
      <input
        id={id}
        aria-describedby={hint ? hintId : undefined}
        aria-invalid={error ? true : undefined}
        className="rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm text-ink
          placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-offset-1
          focus-visible:outline-accent"
        {...rest}
      />
      {error ? <p className="text-xs text-accent">{error}</p> : null}
    </div>
  );
}
