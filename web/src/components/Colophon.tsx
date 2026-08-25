import { PRODUCT } from "@app/lib/constants";

/**
 * What this is and who made it.
 *
 * At the bottom of the reader, under the sign-in card, and on a page somebody published —
 * which is the one of the three a stranger reaches, and so the only one where it is doing the
 * whole job rather than half of it.
 *
 * Quiet on purpose. It is a colophon and not a banner: the line at the end of a book that says
 * who set the type, which anybody looking for it finds and nobody else is troubled by.
 */
export function Colophon({ className = "" }: { className?: string }) {
  return (
    <p className={`text-xs text-ink-faint ${className}`}>
      <a
        href={PRODUCT.url}
        target="_blank"
        rel="noopener noreferrer"
        className="underline underline-offset-2 hover:text-ink"
      >
        {PRODUCT.name}
      </a>{" "}
      by{" "}
      <a
        href={PRODUCT.author.url}
        target="_blank"
        rel="noopener noreferrer"
        className="underline underline-offset-2 hover:text-ink"
      >
        {PRODUCT.author.name}
      </a>
    </p>
  );
}
