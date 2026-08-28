import { Logo } from "@app/components/Logo";
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
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-center gap-x-6 gap-y-3 px-6 py-5">
        {/* Sized by height; the lockup is a little over five to one, so the width follows.
            Shorter than the 30px the wordmark was set at, because the logo carries the article
            above the name and the head beside it — the same type in this arrangement is half
            again as tall a block. */}
        {/* The row is centred, so the logo is set at its whole box — the one that runs from
            the top of the article to the tail of the y. Cropping it to the baseline would
            centre a box that is not the drawing, which puts the drawing low by the tail.

            35px, which is the 28 the name was set at plus the descender the box gets back. */}
        <a
          href="/"
          aria-label="the bystander"
          className="inline-flex text-ink hover:text-accent"
        >
          <Logo className="h-[35px] w-auto" />
        </a>
        <div className="flex basis-full items-baseline gap-4 text-sm sm:ml-auto sm:basis-auto">
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
