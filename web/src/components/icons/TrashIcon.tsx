/**
 * A bin, at the size of the text beside it.
 *
 * The mark on the one control here that cannot be undone. It is never the only thing saying
 * so — the button says what it does and asks before doing it — but a bin is read before a
 * word is, which is worth having on the row somebody is scanning quickly.
 */
export function TrashIcon({ className = "" }: { className?: string }) {
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
      <path d="M2.4 4.2h11.2" />
      <path d="M6.1 4.2V2.9a.9.9 0 0 1 .9-.9h2a.9.9 0 0 1 .9.9v1.3" />
      <path d="M3.7 4.2 4.35 13a1 1 0 0 0 1 .95h5.3a1 1 0 0 0 1-.95l.65-8.8" />
      <path d="M6.6 6.8v4.6M9.4 6.8v4.6" />
    </svg>
  );
}
