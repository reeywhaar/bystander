import { useState } from "react";

import type { MarkSpan } from "@app/api/actions/feeds";
import type { Subscription } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { useMarkFeedRead } from "@app/queries/hooks";

/**
 * How far back to mark, and what each one is for.
 *
 * A closed set rather than a date picker, for the same reason every other duration here is one:
 * four choices fit in a dialog, and nobody wants to pick a date to say "I have read the old
 * ones".
 */
const SPANS: { value: MarkSpan; label: string; what: string }[] = [
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
    what: "The whole feed, including what no page has shown yet.",
  },
];

/**
 * Marks a feed's articles read, as far back as somebody chooses.
 *
 * The thing worth knowing before pressing it is that this reaches further than the page in
 * front of you. A page never offers an article this person has already read, so marking a
 * feed's backlog read means those articles are never drawn at all — which is exactly what
 * somebody wants after following a publisher again, or after reading it somewhere else for a
 * month, and exactly what they do not want if they thought it only greyed what was on screen.
 * So the dialog says so rather than leaving it to be discovered.
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
  const [span, setSpan] = useState<MarkSpan>("week");
  const mark = useMarkFeedRead();
  const [marked, setMarked] = useState<number | null>(null);

  const apply = () =>
    mark.mutate(
      { id: feed.id, olderThan: span },
      { onSuccess: (result) => setMarked(result.marked) },
    );

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Mark ${feed.title} read`}
      footer={
        marked === null ? (
          <>
            <Button onClick={onClose} disabled={mark.isPending}>
              Cancel
            </Button>
            <Button variant="primary" onClick={apply} disabled={mark.isPending}>
              {mark.isPending ? "Marking…" : "Mark read"}
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
            screen — so what is marked here will not turn up on a later page.
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
                  checked={span === option.value}
                  onChange={() => setSpan(option.value)}
                />
                <span>
                  <span className="text-ink">{option.label}</span>{" "}
                  <span className="text-xs text-ink-faint">{option.what}</span>
                </span>
              </label>
            ))}
          </div>

          {mark.error ? <Alert>{mark.error.message}</Alert> : null}
        </div>
      ) : (
        <p className="text-sm text-ink-muted">
          {/* The count is worth saying: "nothing" and "four hundred" are very different
              outcomes of the same press, and only one of them means it did nothing.
              
              Nothing covers two cases and does not try to tell them apart, because the
              server counts rows rather than reasons: there was nothing that old, or what
              was that old had been read already. Either way there was nothing to do. */}
          {marked === 0
            ? "Nothing to mark — nothing that old, or it had been read already."
            : `Marked ${marked} ${marked === 1 ? "article" : "articles"} read.`}
        </p>
      )}
    </Modal>
  );
}
