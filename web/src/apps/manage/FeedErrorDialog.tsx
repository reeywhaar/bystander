import type { Subscription } from "@app/api/types";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { since } from "@app/lib/time";

/**
 * Why a feed has stopped answering, in the words of whatever refused.
 *
 * This lived in a `title` attribute, which is a tooltip: it needs a pointer to hover, so on a
 * phone the answer to "why is this feed not answering" was unreachable. It also held only the
 * summary line, and the summary line is the half nobody can act on — "the server answered 503"
 * is a fact, and the body underneath it is the reason.
 *
 * Two situations, and they need to be told apart before anything else is said. A request that
 * never reached a server is a name that will not resolve, a refused connection, a timeout —
 * something between here and there. A server that answered and refused is a decision, and it
 * usually explains itself: a rate-limit note, a feed that has moved, a login page where a feed
 * used to be. `last_status` is what separates them, by being zero in the first case.
 */
export function FeedErrorDialog({
  feed,
  open,
  onClose,
}: {
  feed: Subscription;
  open: boolean;
  onClose: () => void;
}) {
  const answered = feed.last_status > 0;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={feed.title}
      footer={<Button onClick={onClose}>Close</Button>}
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-ink-muted">
          {answered ? (
            <>
              The server answered, and refused — with{" "}
              <span className="text-ink">{feed.last_status}</span>. That is a
              decision at their end rather than a problem reaching them.
            </>
          ) : (
            <>
              Nothing answered. The request did not reach a server at all — a
              name that will not resolve, a refused connection, or a wait that
              ran out.
            </>
          )}{" "}
          {feed.failure_count === 1
            ? "This is the first failure."
            : `It has failed ${feed.failure_count} times in a row.`}{" "}
          {feed.last_success_at
            ? `It last worked ${since(feed.last_success_at)}.`
            : "It has never worked."}
        </p>

        <Field label="What went wrong">{feed.last_error}</Field>

        {/* Only when there is one. A request that never arrived has no answer to show, and an
            empty box under a heading reads as something missing rather than as nothing said. */}
        {feed.last_error_body ? (
          <Field label="What the server said">{feed.last_error_body}</Field>
        ) : null}

        <p className="text-xs text-ink-faint">
          Fetching is tried again on its own, less often each time it fails.
          Nothing here needs doing.
        </p>
      </div>
    </Modal>
  );
}

/**
 * One labelled block of the server's own words.
 *
 * `break-words` breaks a word only when the word alone will not fit, which is what an error
 * body needs both ways round: prose wraps at its spaces, and one unbroken four-hundred
 * character URL breaks rather than running off the side. `break-all` was tried and breaks
 * everything at the margin, so a sentence came out as "…too often. T / ry again in an hour."
 */
function Field({ label, children }: { label: string; children: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm font-medium text-ink">{label}</span>
      <pre className="max-h-56 overflow-auto rounded-md border border-rule bg-paper p-3 font-mono text-xs break-words whitespace-pre-wrap text-ink-muted">
        {children}
      </pre>
    </div>
  );
}
