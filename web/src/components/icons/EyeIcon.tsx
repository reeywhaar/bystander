/**
 * An eye, at the size of the text beside it.
 *
 * `1em` and `currentColor`, like the rest of them: it sits in a row of small type next to a
 * feed's weight, and an icon drawn at a fixed size is an icon that looks wrong at every size
 * but the one it was drawn for.
 *
 * Looking, not watching. This is the mark on the control that shows what a feed is publishing
 * without following it anywhere — the oldest picture there is for "let me see that first",
 * and the one thing here that costs nothing to press.
 */
export function EyeIcon({ className = "" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      width="1em"
      height="1em"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
      strokeLinejoin="round"
      // Decorative: the button around it carries the name, so a label here would have a
      // screen reader say it twice.
      aria-hidden="true"
      className={className}
    >
      <path d="M1.5 8S4.4 3.5 8 3.5 14.5 8 14.5 8 11.6 12.5 8 12.5 1.5 8 1.5 8Z" />
      <circle cx="8" cy="8" r="2" />
    </svg>
  );
}
