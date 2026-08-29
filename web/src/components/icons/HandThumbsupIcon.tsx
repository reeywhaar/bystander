/**
 * A thumb up, at the size of the text beside it.
 *
 * "More of this." Paired with [HandThumbsdownIcon] and drawn as its mirror, so the two read
 * as one control with two directions rather than as two unrelated pictures.
 */
export function HandThumbsupIcon({ className = "" }: { className?: string }) {
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
      aria-hidden="true"
      className={className}
    >
      {/* The cuff: the part of the hand nearest the wrist, drawn as its own box so the
          thumb reads as coming out of a fist rather than out of a blob. */}
      <path d="M1.6 7.4h2.6v6.4H1.6z" />
      <path d="M4.2 7.4 7 1.9a1.6 1.6 0 0 1 2.3 1.9L8.5 6.4h4a1.6 1.6 0 0 1 1.55 2l-1.2 4.2a1.6 1.6 0 0 1-1.55 1.2H4.2Z" />
    </svg>
  );
}
