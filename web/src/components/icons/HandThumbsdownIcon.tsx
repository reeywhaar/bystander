/**
 * A thumb down, at the size of the text beside it.
 *
 * "Less of this." [HandThumbsupIcon] turned through half a turn rather than drawn again — a
 * pair that is not exactly a mirror reads as a pair somebody got slightly wrong, and this is
 * the one place in the product where two icons are looked at side by side.
 */
export function HandThumbsdownIcon({ className = "" }: { className?: string }) {
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
      <g transform="rotate(180 8 8)">
        <path d="M1.6 7.4h2.6v6.4H1.6z" />
        <path d="M4.2 7.4 7 1.9a1.6 1.6 0 0 1 2.3 1.9L8.5 6.4h4a1.6 1.6 0 0 1 1.55 2l-1.2 4.2a1.6 1.6 0 0 1-1.55 1.2H4.2Z" />
      </g>
    </svg>
  );
}
