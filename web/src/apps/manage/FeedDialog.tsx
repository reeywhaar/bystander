import { useEffect, useState } from "react";

import type { Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { ARTICLE_WINDOWS } from "@app/lib/constants";
import { tagLabel } from "@app/lib/tags";
import { useRemoveFeed, useUpdateFeed } from "@app/queries/hooks";

/**
 * Everything about one feed, in one place.
 *
 * This was an accordion under the row, which put a page of controls between one feed and
 * the next and made the list impossible to scan while any of it was open. A feed's settings
 * are something you go and change, not something you want spread through a list you are
 * reading.
 *
 * The name, the filing and the reach are all here, so there is one thing to open rather
 * than a pencil for one of them and a disclosure for the rest.
 */
export function FeedDialog({
  feed,
  tags,
  onClose,
}: {
  feed: Subscription | null;
  tags: Tag[];
  onClose: () => void;
}) {
  const update = useUpdateFeed();
  const remove = useRemoveFeed();

  // Held here until Save, rather than written as you touch things.
  //
  // A dialog that saves on every click has no way to be closed without consequences, and
  // each toggle becomes a request the list underneath has to catch up with. These are
  // choices somebody makes together and confirms once.
  const [name, setName] = useState("");
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [reach, setReach] = useState(0);

  // Keyed on which feed, not on the feed object. The object changes identity every time
  // the list refetches — which is after every change made in here — and resetting on that
  // would wipe out whatever was being typed.
  const openFor = feed?.id;
  useEffect(() => {
    if (!feed) return;
    setName(feed.title);
    setTagIDs(feed.tag_ids);
    setReach(feed.article_window);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openFor]);

  if (!feed) return null;

  const publisher = feed.feed_title || feed.url;

  function save() {
    if (!feed) return;

    // Empty is refused rather than treated as a reset — a feed with no name at all is not
    // something anybody means to ask for, and "Use publisher title" is the way back.
    const trimmed = name.trim();
    if (trimmed === "") return;

    // Typing the publisher's title out is the same as having no override: there is no
    // reason to store one that says the same thing as the title it overrides.
    const override = trimmed === publisher ? "" : trimmed;

    const sameTags =
      tagIDs.length === feed.tag_ids.length &&
      tagIDs.every((id) => feed.tag_ids.includes(id));

    if (
      sameTags &&
      reach === feed.article_window &&
      override === feed.title_override
    ) {
      onClose();
      return;
    }

    update.mutate(
      {
        id: feed.id,
        changes: {
          title_override: override,
          tag_ids: tagIDs,
          article_window: reach,
        },
      },
      { onSuccess: onClose },
    );
  }

  return (
    <Modal open onClose={onClose} title={feed.title}>
      <div className="flex flex-col gap-1.5">
        <Field
          label="What to call it"
          value={name}
          maxLength={200}
          placeholder={publisher}
          onChange={(event) => setName(event.target.value)}
        />

        {/* A way back to the publisher's title that does not require knowing it, or
            retyping it. It fills the field rather than saving, so it is a suggestion to
            look at rather than a decision already taken — and it goes once the field
            already says that. */}
        {name.trim() !== publisher ? (
          <button
            type="button"
            onClick={() => setName(publisher)}
            className="self-start border-b border-dashed border-ink-faint text-xs
              text-ink-muted hover:border-ink hover:text-ink"
          >
            Use publisher title
          </button>
        ) : null}
      </div>

      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-ink-muted">Filed under</p>
        <div className="flex flex-wrap items-center gap-1.5">
          {tags.length === 0 ? (
            <p className="text-xs text-ink-faint">
              No tags yet. Tags are how you say which kinds of thing appear more
              often.
            </p>
          ) : (
            tags.map((tag) => {
              const on = tagIDs.includes(tag.id);
              return (
                <button
                  key={tag.id}
                  type="button"
                  aria-pressed={on}
                  onClick={() =>
                    setTagIDs((was) =>
                      on ? was.filter((id) => id !== tag.id) : [...was, tag.id],
                    )
                  }
                  className={`rounded-full border px-2.5 py-0.5 text-xs ${
                    on
                      ? "border-accent bg-accent/10 text-accent"
                      : "border-rule text-ink-muted hover:text-ink"
                  }`}
                >
                  {tagLabel(tags, tag.id)}
                </button>
              );
            })
          )}
        </div>
      </div>

      {/* How far back a page reaches into *this* feed. A news site worth a day and a blog
          that posts monthly are exactly the pair one number cannot serve. */}
      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-ink-muted">
          Reaches back — articles older than this are not picked from this feed
        </p>
        <div className="flex flex-wrap gap-1.5">
          {ARTICLE_WINDOWS.map((window) => {
            const on = window.seconds === reach;
            return (
              <button
                key={window.seconds}
                type="button"
                aria-pressed={on}
                onClick={() => setReach(window.seconds)}
                className={`rounded-md border px-2.5 py-1 text-xs ${
                  on
                    ? "border-accent bg-accent/10 text-accent"
                    : "border-rule text-ink-muted hover:text-ink"
                }`}
              >
                {window.label}
              </button>
            );
          })}
        </div>
      </div>

      <p className="text-xs break-all text-ink-faint select-all">{feed.url}</p>

      {feed.failure_count > 0 ? <Alert>{feed.last_error}</Alert> : null}
      {update.error ? <Alert>{update.error.message}</Alert> : null}
      {remove.error ? <Alert>{remove.error.message}</Alert> : null}

      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button
          variant="danger"
          disabled={remove.isPending}
          onClick={() => remove.mutate(feed.id, { onSuccess: onClose })}
        >
          Stop following
        </Button>
        <span className="flex gap-2">
          {/* Closing any other way — Cancel, Escape, the backdrop — leaves the feed as it
              was. Save is the only thing that writes. */}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            onClick={save}
            disabled={name.trim() === "" || update.isPending}
          >
            {update.isPending ? "Saving…" : "Save"}
          </Button>
        </span>
      </div>
    </Modal>
  );
}
