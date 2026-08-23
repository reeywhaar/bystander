import { useEffect, useState, type FormEvent } from "react";

import type { Subscription } from "@app/api/types";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";

/**
 * Gives a feed a better name.
 *
 * Some publishers title their feed "technology archives | designboom | architecture &
 * design magazine", which is unreadable in a list and worse on a card. The new name is the
 * subscriber's, not the feed's: two people following the same feed can call it different
 * things, and the publisher's own title goes on being fetched underneath.
 */
export function RenameDialog({
  feed,
  onClose,
  onSave,
  saving,
}: {
  feed: Subscription | null;
  onClose: () => void;
  onSave: (title: string) => void;
  saving: boolean;
}) {
  const [name, setName] = useState("");

  // Starts from what it is called now, whichever of the two that is.
  useEffect(() => {
    if (feed) setName(feed.title);
  }, [feed]);

  if (!feed) return null;

  const publisher = feed.feed_title || feed.url;
  const overridden = name.trim() !== "" && name.trim() !== publisher;

  function submit(event: FormEvent) {
    event.preventDefault();
    // Empty puts the publisher's name back rather than leaving a blank, and so does typing
    // the publisher's name out — there is no reason to store an override that says the
    // same thing.
    const trimmed = name.trim();
    onSave(trimmed === publisher ? "" : trimmed);
  }

  return (
    <Modal open onClose={onClose} title="Rename this feed">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field
          label="What to call it"
          value={name}
          autoFocus
          maxLength={200}
          onChange={(event) => setName(event.target.value)}
          hint={
            <>
              The publisher calls it{" "}
              <span className="text-ink">{publisher}</span>. This name is yours
              alone.
            </>
          }
        />

        <div className="flex flex-wrap justify-end gap-2">
          {feed.title_override !== "" || overridden ? (
            <Button
              onClick={() => setName(publisher)}
              disabled={saving || name.trim() === publisher}
            >
              Use theirs
            </Button>
          ) : null}
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
