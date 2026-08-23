import { useEffect, useState, type FormEvent } from "react";

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
  const [name, setName] = useState("");

  // Starts from what it is called now, whichever of the two that is.
  useEffect(() => {
    if (feed) setName(feed.title);
  }, [feed]);

  if (!feed) return null;

  const publisher = feed.feed_title || feed.url;

  function saveName(event: FormEvent) {
    event.preventDefault();
    if (!feed) return;
    // Empty puts the publisher's name back, and so does typing theirs out: there is no
    // reason to store an override that says the same thing.
    const trimmed = name.trim();
    update.mutate({
      id: feed.id,
      changes: { title_override: trimmed === publisher ? "" : trimmed },
    });
  }

  const renamed = name.trim() !== feed.title;

  return (
    <Modal open onClose={onClose} title={feed.title}>
      <form onSubmit={saveName} className="flex items-end gap-2">
        <Field
          label="What to call it"
          value={name}
          maxLength={200}
          className="flex-1"
          onChange={(event) => setName(event.target.value)}
          hint={
            <>
              The publisher calls it{" "}
              <span className="text-ink">{publisher}</span>. This name is yours
              alone.
            </>
          }
        />
        <Button type="submit" disabled={!renamed || update.isPending}>
          Rename
        </Button>
      </form>

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
              const on = feed.tag_ids.includes(tag.id);
              return (
                <button
                  key={tag.id}
                  type="button"
                  aria-pressed={on}
                  onClick={() =>
                    update.mutate({
                      id: feed.id,
                      changes: {
                        tag_ids: on
                          ? feed.tag_ids.filter((id) => id !== tag.id)
                          : [...feed.tag_ids, tag.id],
                      },
                    })
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
            const on = window.seconds === feed.article_window;
            return (
              <button
                key={window.seconds}
                type="button"
                aria-pressed={on}
                onClick={() =>
                  update.mutate({
                    id: feed.id,
                    changes: { article_window: window.seconds },
                  })
                }
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
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </div>
    </Modal>
  );
}
