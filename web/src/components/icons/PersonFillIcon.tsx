/**
 * A person, at the size of the text beside it: `1em`, `currentColor`, no label of its own.
 *
 * Filled, which is what the `Fill` in the name is for — see the icon naming rule in
 * docs/frontend.md. A hairline stroke reads lighter than the word it stands next to; a
 * silhouette carries the same weight as the type.
 */
export function PersonFillIcon({ className = "" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      width="1em"
      height="1em"
      fill="currentColor"
      // Decorative: the name is right there, so a label here would have a screen reader
      // say it twice.
      aria-hidden="true"
      className={className}
    >
      <circle cx="8" cy="4.9" r="2.9" />
      {/* Shoulders: a half disc, closed along its own diameter. The gap between it and the
          head is what keeps the two shapes legible as two at a dozen pixels. */}
      <path d="M2.4 14a5.6 5.6 0 0 1 11.2 0Z" />
    </svg>
  );
}
