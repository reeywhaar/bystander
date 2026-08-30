import { useState } from "react";

import type { MarkSpan } from "@app/api/actions/feeds";
import type { Subscription } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { useMarkFeedRead, useUnmarkFeedRead } from "@app/queries/hooks";

/**
 * How far back to mark, and what each one is for.
 *
 * A closed set rather than a date picker, for the same reason every other duration here is one:
 * four choices fit in a dialog, and nobody wants to pick a date to say "I have read the old
 * ones".
 */
/** What the dialog can do: a span to mark read, or the whole thing the other way. */
type Choice = MarkSpan | "unread";

const SPANS: { value: Choice; label: string; what: string }[] = [
  {
    value: "day",
    label: "Older than a day",
    what: "Keeps today's, clears the rest.",
  },
  {
    value: "week",
    label: "Older than a week",
    what: "Keeps this week's.",
  },
  {
    value: "month",
    label: "Older than a month",
    what: "Keeps the last month's.",
  },
  {
    value: "",
    label: "Everything",
    what: "The whole feed.",
  },
  {
    value: "unread",
    label: "Mark it all unread",
    what: "Forgets that any of it was read, and offers it again.",
  },
];

/**
 * Marks a feed's articles read, as far back as somebody chooses.
 *
 * The thing worth knowing before pressing it is that this reaches further than the page in
 * front of you: it covers the backlog no page has shown yet, so following a publisher again
 * starts from now rather than from its archive. That is exactly what somebody wants after
 * coming back to a feed, and exactly what they do not want if they thought it only greyed what
 * was on screen. So the dialog says so rather than leaving it to be discovered.
 *
 * "Drops behind" rather than "never drawn again", which is the honest version. Read articles
 * are the sampler's last band — see internal/edition/select.go — so they are drawn only when
 * everything unread has run out. On a page with other feeds that is never; on a page with
 * nothing else left it is the difference between a shuffled page and a blank one.
 *
 * A dialog rather than a confirmation on a button: the question is not "are you sure" but "how
 * much", and a dialog that asks the real question does not need the other one.
 */
export function MarkReadDialog({
  feed,
  open,
  onClose,
}: {
  feed: Subscription;
  open: boolean;
  onClose: () => void;
}) {
  const [choice, setChoice] = useState<Choice>("week");
  const mark = useMarkFeedRead();
  const unmark = useUnmarkFeedRead();
  const [marked, setMarked] = useState<number | null>(null);

  const undoing = choice === "unread";
  const busy = mark.isPending || unmark.isPending;
  const failure = mark.error ?? unmark.error;

  const done = (result: { marked: number }) => setMarked(result.marked);
  const apply = () =>
    undoing
      ? unmark.mutate(feed.id, { onSuccess: done })
      : mark.mutate({ id: feed.id, olderThan: choice }, { onSuccess: done });

  return (
    <Modal
      open={open}
      onClose={onClose}
      // Not "…read": one of the choices below does the opposite, and a title that names
      // only one of them is a title arguing with the option somebody just picked.
      title={`Mark ${feed.title}`}
      footer={
        marked === null ? (
          <>
            <Button onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            {/* The label follows the choice rather than staying "Mark read" over an option
                that does the opposite. */}
            <Button variant="primary" onClick={apply} disabled={busy}>
              {busy ? "Marking…" : undoing ? "Mark unread" : "Mark read"}
            </Button>
          </>
        ) : (
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        )
      }
    >
      {marked === null ? (
        <div className="flex flex-col gap-4">
          <p className="text-sm text-ink-muted">
            This covers articles no page has shown you yet, not only the ones on
            screen — so what is marked here drops behind everything else and
            stops competing for a place on later pages.
          </p>

          <div className="flex flex-col gap-1">
            {SPANS.map((option) => (
              <label
                key={option.value || "all"}
                className="flex cursor-pointer items-baseline gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-ink/5"
              >
                <input
                  type="radio"
                  name="span"
                  className="accent-accent"
                  checked={choice === option.value}
                  onChange={() => setChoice(option.value)}
                />
                <span>
                  <span className="text-ink">{option.label}</span>{" "}
                  <span className="text-xs text-ink-faint">{option.what}</span>
                </span>
              </label>
            ))}
          </div>

          {failure ? <Alert>{failure.message}</Alert> : null}
        </div>
      ) : (
        <p className="text-sm text-ink-muted">
          {/* The count is worth saying: "nothing" and "four hundred" are very different
              outcomes of the same press, and only one of them means it did nothing.
              
              Nothing covers two cases and does not try to tell them apart, because the
              server counts rows rather than reasons: there was nothing that old, or what
              was that old had been read already. Either way there was nothing to do. */}
          {marked === 0
            ? undoing
              ? "Nothing to forget — none of it was marked read."
              : "Nothing to mark — nothing that old, or it had been read already."
            : `Marked ${marked} ${marked === 1 ? "article" : "articles"} ${
                undoing ? "unread" : "read"
              }.`}
        </p>
      )}
    </Modal>
  );
}
