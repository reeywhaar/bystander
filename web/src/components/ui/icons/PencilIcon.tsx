/**
 * Inline SVG, one icon per file. Not an icon font and not a package: this is a handful of
 * paths, and a dependency for them would be larger than all of them together.
 *
 * `currentColor` throughout, so an icon is coloured by the text around it.
 */
export function PencilIcon({ className = "size-3.5" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      <path d="M11.5 2.5a1.4 1.4 0 0 1 2 2L5 13l-3 1 1-3 8.5-8.5Z" />
    </svg>
  );
}
