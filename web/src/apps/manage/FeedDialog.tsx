import { useEffect, useId, useState } from "react";

import type { Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";

import { MarkReadDialog } from "@app/apps/manage/MarkReadDialog";
import { NewTagDialog } from "@app/apps/manage/NewTagDialog";
import { TagChips } from "@app/apps/manage/TagChips";
import { ARTICLE_WINDOWS } from "@app/lib/constants";
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
  // Generated rather than fixed, because two of these on one page would otherwise share an
  // id and the label would point at whichever mounted first.
  const noteID = useId();

  // Held here until Save, rather than written as you touch things.
  //
  // A dialog that saves on every click has no way to be closed without consequences, and
  // each toggle becomes a request the list underneath has to catch up with. These are
  // choices somebody makes together and confirms once.
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [reach, setReach] = useState(0);
  const [marking, setMarking] = useState(false);
  const [makingTag, setMakingTag] = useState(false);

  // Keyed on which feed, not on the feed object. The object changes identity every time
  // the list refetches — which is after every change made in here — and resetting on that
  // would wipe out whatever was being typed.
  const openFor = feed?.id;
  useEffect(() => {
    if (!feed) return;
    setName(feed.title);
    setNote(feed.note);
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

    // Trimmed before comparing as well as before sending, so opening the dialog and pressing
    // Save without touching anything is not a write because of a trailing newline.
    const written = note.trim();

    if (
      sameTags &&
      reach === feed.article_window &&
      override === feed.title_override &&
      written === feed.note
    ) {
      onClose();
      return;
    }

    update.mutate(
      {
        id: feed.id,
        changes: {
          title_override: override,
          note: written,
          tag_ids: tagIDs,
          article_window: reach,
        },
      },
      { onSuccess: onClose },
    );
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={feed.title}
      footer={
        <>
          {/* Unfollowing belongs to neither group, so it sits away from both. */}
          <Button
            className="mr-auto"
            variant="danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(feed.id, { onSuccess: onClose })}
          >
            Stop following
          </Button>
          {/* Neither a save nor a dismissal: it writes, but not to the feed. It sits with
              Cancel and Save because it is the third thing somebody opens this to do. */}
          <Button onClick={() => setMarking(true)} disabled={update.isPending}>
            Mark read…
          </Button>
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
        </>
      }
    >
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

      {/* Why this one is here, which is the one thing about a feed nothing else can say.
          A name is the publisher's and the tags are a filing system; this is the sentence
          that answers "why am I still reading this" a year later, and it is the difference
          between unfollowing something confidently and keeping it in case it mattered.

          Optional, and empty on almost every feed. Required, it would be forty sentences
          written to satisfy a form and not one of them worth reading. */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor={noteID} className="text-sm font-medium text-ink">
          Why you read it
        </label>
        <p id={`${noteID}-hint`} className="text-xs text-ink-muted">
          A note to yourself, shown under the feed in the list. Nobody else sees
          it.
        </p>
        <textarea
          id={noteID}
          aria-describedby={`${noteID}-hint`}
          value={note}
          rows={3}
          maxLength={500}
          placeholder="Why this is worth following"
          onChange={(event) => setNote(event.target.value)}
          className="resize-y rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm
            text-ink placeholder:text-ink-faint focus-visible:outline-2
            focus-visible:outline-offset-1 focus-visible:outline-accent"
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-ink-muted">Filed under</p>
        {/* Making one from here rather than sending somebody to the tags page and back:
            this is the moment they know where the feed belongs, and everything typed in
            this dialog is unsaved until Save — leaving would throw it away. */}
        <TagChips
          tags={tags}
          chosen={tagIDs}
          onToggle={(id) =>
            setTagIDs((was) =>
              was.includes(id)
                ? was.filter((other) => other !== id)
                : [...was, id],
            )
          }
          onNew={() => setMakingTag(true)}
        />
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
      {/* Mounted only while it is open, so the feed dialog is not carrying a second one
          around unopened. */}
      {marking ? (
        <MarkReadDialog feed={feed} open onClose={() => setMarking(false)} />
      ) : null}
      {/* Ticked on the way out, because the only reason to make a tag from in here is to
          file this feed under it. */}
      {makingTag ? (
        <NewTagDialog
          open
          tags={tags}
          onClose={() => setMakingTag(false)}
          onCreated={(tag) => setTagIDs((was) => [...was, tag.id])}
        />
      ) : null}
    </Modal>
  );
}
