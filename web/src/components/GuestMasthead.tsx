/**
 * The band across the top for somebody without a session.
 *
 * The application's own masthead needs a `Me` to show, so the two documents a stranger can be
 * looking at — a published page, and the landing page at "/" — would otherwise each grow their
 * own header. They are the same header: the wordmark, and a way in.
 */
export function GuestMasthead({ onSignIn }: { onSignIn: () => void }) {
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
        </div>
      </div>
    </header>
  );
}
