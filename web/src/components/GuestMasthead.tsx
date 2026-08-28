import { GitHubIcon } from "@app/components/icons/GitHubIcon";
import { PRODUCT } from "@app/lib/constants";

/**
 * The band across the top for somebody without a session.
 *
 * The application's own masthead needs a `Me` to show, so the two documents a stranger can be
 * looking at — a published page, and the landing page at "/" — would otherwise each grow their
 * own header. They are the same header: the wordmark, and a way in.
 */
export function GuestMasthead({
  onSignIn,
  source = false,
}: {
  onSignIn: () => void;
  /**
   * Whether to offer the source alongside the way in.
   *
   * Off by default, and off on a published page. That document is somebody's front page shown
   * to a stranger who came for what is on it, and a link to the software it was made with is
   * an advertisement in the top corner of their page. It belongs on the one document that
   * exists to make the argument — where it is the first thing somebody convinced by the
   * argument will go looking for.
   */
  source?: boolean;
}) {
  return (
    <header className="border-b border-rule">
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-baseline gap-x-6 gap-y-1 px-6 py-5">
        <a href="/" className="nameplate text-ink hover:text-accent">
          bystander
        </a>
        <div className="flex basis-full items-center gap-4 text-sm sm:ml-auto sm:basis-auto">
          {/* A button, not a link to the login island. Signing in here is not the errand —
              they are reading a page, and being sent away and brought back would lose their
              place in it for a gesture made in passing. */}
          <button
            type="button"
            onClick={onSignIn}
            className="text-ink-muted hover:text-ink"
          >
            Sign in
          </button>

          {/* Before the wordmark's own business rather than after it, because somebody who
              has just read what this is wants to see it and somebody with an account is
              already reaching for Sign in without looking. */}
          {source ? (
            <a
              href={PRODUCT.url}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1.5 text-ink-muted hover:text-ink"
            >
              <GitHubIcon />
              GitHub
            </a>
          ) : null}
        </div>
      </div>
    </header>
  );
}
