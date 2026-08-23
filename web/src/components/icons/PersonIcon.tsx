/**
 * A person, at the size of the text beside it.
 *
 * `1em` rather than a fixed size, and `currentColor` rather than a fill: it sits inline in
 * the masthead next to a name, and an icon that does not change with the type it sits
 * beside is an icon that looks wrong at every size but one.
 */
export function PersonIcon({ className = "" }: { className?: string }) {
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
      // Decorative: the name is right there, so a label here would have a screen reader
      // say it twice.
      aria-hidden="true"
      className={className}
    >
      <circle cx="8" cy="5" r="2.75" />
      <path d="M2.75 14a5.25 5.25 0 0 1 10.5 0" />
    </svg>
  );
}
