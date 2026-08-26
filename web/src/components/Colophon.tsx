import { PRODUCT } from "@app/lib/constants";

/**
 * What this is and who made it.
 *
 * At the bottom of every document this program serves: the reader, the two it is managed
 * from, the sign-in card, a page somebody published, and the error state — which is the one
 * that most needs it, since a boundary replaces a whole island and would otherwise leave that
 * document saying nothing about itself anywhere.
 *
 * The published page is where it does the whole job rather than half of it, being the only one
 * a stranger reaches. Everywhere else it is for the person who wonders what they are looking
 * at, or wants somewhere to report something to.
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
