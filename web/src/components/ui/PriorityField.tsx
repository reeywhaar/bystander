import type { ReactNode } from "react";

import { Priority } from "@app/components/ui/Priority";

/**
 * A priority as a field: the value on one line, a track filling the width beneath it, both
 * inside a box.
 *
 * Beside other fields this is what a slider has to be. Bare, it is two pieces of text and a
 * rule sitting next to things that draw boxes, which reads as a caption on a control rather
 * than as a control with a label — and inline it puts a short track in the middle of whatever
 * width it was given, leaving the smallest thing in the row to aim at.
 *
 * The box wants a width from its caller and takes none of its own, because the two places
 * this appears want different ones: a phone gives it the whole line, a screen a column. It is
 * the one field in either list with somewhere useful to spend extra width — a longer track
 * sets a finer value, where a longer menu says nothing more.
 *
 * The raised ground is for a phone, where a row is a stack of fields and each of them should
 * read as one — in the tag list those rows are cards on sunken paper, and a field that shared
 * their ground would disappear into it. On a screen the rows are ruled and the page is the
 * ground everything sits on, so it goes.
 */
export function PriorityField({
  value,
  onChange,
  label,
  leading,
  className = "",
}: {
  value: number;
  onChange: (value: number) => void;
  label: string;
  /** Something to sit before the value, inside the label line — see [Priority]. */
  leading?: ReactNode;
  /** Where it sits and how wide it is, which only the layout around it knows. */
  className?: string;
}) {
  return (
    <span
      className={`block rounded-md border border-rule bg-paper-raised px-2 py-1
        sm:bg-transparent ${className}`}
    >
      <Priority
        fill
        label={label}
        value={value}
        onChange={onChange}
        leading={leading}
      />
    </span>
  );
}
