import { useEffect, useState } from "react";

import type { Article } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { HandThumbsdownIcon } from "@app/components/icons/HandThumbsdownIcon";
import { HandThumbsupIcon } from "@app/components/icons/HandThumbsupIcon";
import { TrashIcon } from "@app/components/icons/TrashIcon";
import { describePriority, lessOften, moreOften } from "@app/lib/constants";
import { useDropFeed, useSetRead, useUpdateFeed } from "@app/queries/hooks";

/**
 * What a reader can do about the feed a card came from, without leaving the page.
 *
 * The three things somebody thinks while reading — more of this, less of this, done with
 * this — and nothing else. Renaming it, filing it, choosing how far back it reaches: those
 * are settings, they live in the feed list, and putting them here would turn a reaction into
 * an errand.
 *
 * A dialog rather than three controls on the card. They are pressed rarely and the card is
 * read constantly, so on the card they would be three pieces of furniture under every story
 * for the sake of a gesture made once a week — and the destructive one would be sitting under
 * a headline waiting to be brushed.
 */
export function FeedActionsDialog({
  article,
  onClose,
}: {
  /** The card this was opened from, or null when the dialog is shut. */
  article: Article | null;
  onClose: () => void;
}) {
  const update = useUpdateFeed();
  const setRead = useSetRead();
  const drop = useDropFeed();

  // Whether the destructive one has been asked for and not yet confirmed. A second state of
  // this dialog rather than a dialog on top of it: there is one question outstanding, and
  // stacking a box on a box to ask it makes the first one look like something still to
  // answer.
  const [confirming, setConfirming] = useState(false);

  const openFor = article?.id;
  useEffect(() => {
    setConfirming(false);
    update.reset();
    drop.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openFor]);

  if (!article) return null;

  const feed = article.feed;
  const name = feed.title || "this feed";
  // Empty in two places: somebody else's published page, and an article whose subscription
  // went while its page was live. The card offers no way in for either — see ArticleCard —
  // so this is the answer if something ever opens the dialog anyway, rather than a state a
  // reader is expected to arrive in.
  const following = feed.subscription_id !== "";
  const busy = update.isPending || drop.isPending;

  const more = moreOften(feed.priority);
  const less = lessOften(feed.priority);

  function shift(priority: number, alsoRead: boolean) {
    if (!article) return;
    // Marked first and not waiting on the write. The two are independent — one is about this
    // article and the other about every future one — and the mark is optimistic anyway, so
    // sequencing them would only delay the thing that is already on screen.
    if (alsoRead && article.read_at === null) {
      setRead.mutate({ id: article.id, read: true });
    }
    update.mutate(
      { id: feed.subscription_id, changes: { priority } },
      { onSuccess: onClose },
    );
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={name}
      footer={
        confirming ? (
          <>
            <Button onClick={() => setConfirming(false)} disabled={busy}>
              Keep it
            </Button>
            <Button
              variant="danger"
              disabled={busy}
              onClick={() =>
                drop.mutate(
                  { id: feed.subscription_id, feedID: feed.id },
                  { onSuccess: onClose },
                )
              }
            >
              {drop.isPending ? "Removing…" : "Remove it"}
            </Button>
          </>
        ) : (
          <Button variant="primary" onClick={onClose}>
            Close
          </Button>
        )
      }
    >
      {!following ? (
        <p className="text-sm text-ink-muted">
          There is nothing here to change. Either this is somebody else&rsquo;s
          page, or you have stopped following this source — in which case its
          articles keep their place on a page already composed, and will not be
          on the next.
        </p>
      ) : confirming ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-ink">
            Stop following <span className="font-medium">{name}</span>?
          </p>
          <p className="text-sm text-ink-muted">
            Everything from it is marked read, so what is on this page settles
            rather than sitting there unread, and nothing new arrives. What you
            have read there is forgotten with it — there are no more pages for
            it to be kept off.
          </p>
          <p className="text-sm text-ink-muted">
            Following it again later starts from nothing. This cannot be undone.
          </p>
          {drop.error ? <Alert>{drop.error.message}</Alert> : null}
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {/* Where it stands, before either button is pressed. A control that says "show
              less" without saying less than what is asking somebody to press it and watch,
              which on a page composed every few hours means finding out tomorrow.

              The number as well as the word, because the ladder is finer than the words are:
              5 and 15 are both "rarely", and a sentence that does not change between presses
              reads as a press that did nothing. */}
          <p className="text-sm text-ink-muted">
            Drawn{" "}
            <span className="text-ink">
              {describePriority(feed.priority)} ({feed.priority})
            </span>{" "}
            at the moment.
          </p>

          <Action
            icon={<HandThumbsupIcon />}
            label="Show more"
            detail={`Drawn ${describePriority(more)} (${more}) from now on.`}
            disabled={busy || feed.priority >= 100}
            onClick={() => shift(more, false)}
          />
          {/* And marks this one read, because "less of this" is said about something you have
              finished with — leaving it unread would be asking to be shown it again. */}
          <Action
            icon={<HandThumbsdownIcon />}
            label="Show less"
            detail={`Drawn ${describePriority(less)} (${less}) from now on, and this article is marked read.`}
            disabled={busy || feed.priority <= 0}
            onClick={() => shift(less, true)}
          />
          <Action
            icon={<TrashIcon />}
            label="Remove feed"
            detail="Marks everything from it read and stops following it."
            danger
            disabled={busy}
            onClick={() => setConfirming(true)}
          />

          {update.error ? <Alert>{update.error.message}</Alert> : null}
        </div>
      )}
    </Modal>
  );
}

/** One thing to do: a mark, what it is called, and what it will actually do. */
function Action({
  icon,
  label,
  detail,
  danger = false,
  disabled,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  detail: string;
  danger?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`flex w-full items-start gap-3 rounded-md border border-rule px-3 py-2.5
        text-left disabled:opacity-50 ${
          danger
            ? "text-accent hover:border-accent hover:bg-accent/5"
            : "text-ink hover:border-ink-faint hover:bg-paper-sunken"
        }`}
    >
      {/* Nudged down to the cap height of the label beside it. An icon on a two-line block
          aligned to the top of the box sits above the text it belongs to. */}
      <span className="mt-0.5 shrink-0 text-base">{icon}</span>
      <span className="min-w-0">
        <span className="block text-sm font-medium">{label}</span>
        <span className="block text-xs text-ink-muted">{detail}</span>
      </span>
    </button>
  );
}
