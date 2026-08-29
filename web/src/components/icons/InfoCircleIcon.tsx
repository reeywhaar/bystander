/**
 * An i in a circle, at the size of the text beside it.
 *
 * The mark for "there is more about this here" — which is exactly what it opens: not a
 * setting, but the small set of things a reader can do about the feed a card came from.
 * Outlined rather than filled, because it sits under an article at half weight and a
 * silhouette there would be heavier than the type it belongs to.
 */
export function InfoCircleIcon({ className = "" }: { className?: string }) {
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
      <circle cx="8" cy="8" r="6.35" />
      <path d="M8 7.1v4" />
      <path d="M8 4.6h.01" />
    </svg>
  );
}
