import { useState } from "react";

import type { Session } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { Spinner } from "@app/components/ui/Spinner";
import { exact, since, until } from "@app/lib/time";
import {
  useRevokeOtherSessions,
  useRevokeSession,
  useSessions,
} from "@app/queries/hooks";

/**
 * Every device this account is signed in on, and the way to end any of them.
 *
 * Behind a button rather than on the page, because the list is only interesting when
 * somebody has a reason to be suspicious — and fetching it on every visit would record an
 * access on this very session to answer a question nobody had asked.
 *
 * What each row says is descriptive and none of it is proof. An address is whatever the
 * proxy in front reported and belongs to a network rather than to a person; a browser's
 * name for itself is a sentence it chose. That is enough, because the only question here is
 * "do I recognise this", and the person reading is a better judge of that than any check.
 */
export function SessionsDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const sessions = useSessions(open);
  const revoke = useRevokeSession();
  const revokeOthers = useRevokeOtherSessions();
  // The row awaiting a second press. Ending a session is not undoable and the button is
  // one of several in a list, so it asks — inline, on the row itself, rather than by
  // stacking a second dialog on this one to ask about a thing already named on screen.
  const [confirming, setConfirming] = useState<string | null>(null);

  const listed = sessions.data ?? [];
  const others = listed.filter((s) => !s.current).length;
  const error = revoke.error ?? revokeOthers.error ?? null;

  async function end(session: Session) {
    setConfirming(null);
    await revoke.mutateAsync(session);
    if (session.current) {
      // The session this tab was reading with is gone. A whole-document navigation rather
      // than a route change: "/" is served differently to somebody with a cookie and
      // somebody without, and only the server can decide which of the two this now is.
      window.location.assign("/");
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Where you are signed in"
      wide
      flush
      footer={
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          {others > 0 ? (
            <Button
              variant="danger"
              disabled={revokeOthers.isPending}
              onClick={() => revokeOthers.mutate()}
            >
              {revokeOthers.isPending
                ? "Signing out…"
                : `Sign out the other ${others === 1 ? "one" : others}`}
            </Button>
          ) : null}
        </div>
      }
    >
      {error ? <Alert>{error.message}</Alert> : null}

      {sessions.isPending ? (
        <Spinner />
      ) : sessions.error ? (
        <Alert>{sessions.error.message}</Alert>
      ) : (
        <ul className="flex flex-col">
          {listed.map((session) => (
            <li
              key={session.id}
              className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-rule py-3 last:border-b-0"
            >
              <div className="min-w-0 flex-1">
                <p className="text-sm text-ink">
                  {session.device || "An unfamiliar browser"}
                  {session.current ? (
                    <span className="ml-2 text-xs text-accent">this one</span>
                  ) : null}
                </p>
                {/* The address under the name, because together they are what somebody
                    recognises a session by: the browser says what kind of thing it is and
                    the address says roughly where it was. */}
                <p className="text-xs text-ink-faint">
                  {session.ip || "an address that was not recorded"}
                  {" · signed in "}
                  <span title={exact(session.created_at)}>
                    {since(session.created_at)}
                  </span>
                  {" · lapses "}
                  <span title={exact(session.expires_at)}>
                    {until(session.expires_at)}
                  </span>
                </p>
                {/* Verbatim, under the summary rather than instead of it. The summary is a
                    guess at a string built out of thirty years of compatibility lies, and
                    the only way to check a guess is to see what it was made from.

                    Scrolled sideways rather than truncated, because of *where* these strings
                    differ. Every one of them opens "Mozilla/5.0 (Macintosh; Intel Mac OS X
                    10.15; rv:…) Gecko/…" — thirty years of pretending to be each other — and
                    the part that says which browser this actually is comes last. An ellipsis
                    on the right therefore cut the only word worth reading, and did it to
                    every row identically.

                    No `title` any more: a tooltip was standing in for text that could not be
                    reached, and it can be reached now. A hundred and twenty characters in a
                    tooltip was never a good way to read them anyway. */}
                {session.user_agent ? (
                  <p
                    className="mt-0.5 overflow-x-auto font-mono text-[0.6875rem]
                      whitespace-nowrap text-ink-faint [scrollbar-width:thin]
                      [overscroll-behavior-x:contain]"
                  >
                    {session.user_agent}
                  </p>
                ) : null}
              </div>

              {/* Labelled, because a bare relative time in a row that already says when
                  the session started and when it lapses is one date among three. This is
                  the one people came to read — a session last used four days ago from a
                  city you have never been to is the whole reason for the list. */}
              <span className="shrink-0 text-right text-xs">
                <span className="block text-ink-faint">Last access</span>
                <span
                  className="block text-ink"
                  title={exact(session.last_access)}
                >
                  {since(session.last_access)}
                </span>
              </span>

              {confirming === session.id ? (
                <span className="flex shrink-0 items-center gap-1">
                  <Button variant="ghost" onClick={() => setConfirming(null)}>
                    Keep
                  </Button>
                  <Button
                    variant="danger"
                    disabled={revoke.isPending}
                    onClick={() => void end(session)}
                  >
                    {session.current ? "Sign out here" : "Sign it out"}
                  </Button>
                </span>
              ) : (
                <Button
                  variant="ghost"
                  disabled={revoke.isPending || revokeOthers.isPending}
                  onClick={() => setConfirming(session.id)}
                >
                  Sign out
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Modal>
  );
}
