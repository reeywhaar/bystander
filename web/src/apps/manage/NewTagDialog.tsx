import { useEffect, useState } from "react";

import type { Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { Priority } from "@app/components/ui/Priority";
import { Select } from "@app/components/ui/Select";
import { DEFAULT_PRIORITY } from "@app/lib/constants";
import { useAddTag } from "@app/queries/hooks";

/**
 * Making a tag: what it is called, where it sits, and how often it appears.
 *
 * All three at once, which the field-and-a-button this replaces could not do. A tag made
 * there arrived on its own at the default weight, and the two things that make it worth
 * having — where it belongs and how loud it is — had to be set afterwards from its row, in a
 * list somebody had to find it in again. The row still does that, for changing one's mind
 * later; this is for saying it once.
 *
 * A dialog rather than a wider form above the list, because the list is the page. Three
 * controls sitting open at the top of it are three controls in the way of the thing they
 * make, every time somebody comes here to change a priority.
 */
export function NewTagDialog({
  open,
  tags,
  onClose,
  onCreated,
}: {
  open: boolean;
  tags: Tag[];
  onClose: () => void;
  /**
   * The tag that was just made.
   *
   * So a caller can do something with it. Filing a feed opens this precisely because the tag
   * it wants does not exist, and making one and then having to go and tick it is the same
   * dead end one step further along.
   */
  onCreated?: (tag: Tag) => void;
}) {
  const add = useAddTag();
  const { reset } = add;

  const [name, setName] = useState("");
  const [parentID, setParentID] = useState("");
  const [priority, setPriority] = useState(DEFAULT_PRIORITY);

  // Emptied when it opens, not when it closes. A dialog cleared on the way out is one that
  // shows what was typed and abandoned for as long as it takes to close, and this one can be
  // opened from inside another dialog that stays where it is.
  useEffect(() => {
    if (!open) return;
    setName("");
    setParentID("");
    setPriority(DEFAULT_PRIORITY);
    reset();
  }, [open, reset]);

  // Only a root can be a parent: this list nests one deep, and the server refuses a cycle
  // outright. Same rule as a tag's own row, which is where this would be changed later.
  const candidates = tags.filter((tag) => tag.parent_id === null);

  function submit() {
    const trimmed = name.trim();
    if (trimmed === "") return;
    add.mutate(
      { name: trimmed, parent_id: parentID, priority },
      {
        onSuccess: (tag) => {
          onCreated?.(tag);
          onClose();
        },
      },
    );
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New tag"
      footer={
        <>
          <Button onClick={onClose} disabled={add.isPending}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={submit}
            disabled={name.trim() === "" || add.isPending}
          >
            {add.isPending ? "Making…" : "Make it"}
          </Button>
        </>
      }
    >
      <Field
        label="Name"
        value={name}
        maxLength={48}
        autoFocus
        placeholder="World News, Art, Long reads…"
        onChange={(event) => setName(event.target.value)}
      />

      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-ink-muted">Where it sits</p>
        <Select
          small
          value={parentID}
          onChange={(event) => setParentID(event.target.value)}
          aria-label="Where it sits"
        >
          <option value="">on its own</option>
          {candidates.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              under {candidate.name}
            </option>
          ))}
        </Select>
      </div>

      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-ink-muted">
          How often it appears — a probability, not an order. 0 means never.
        </p>
        <Priority
          label="How often it appears"
          value={priority}
          onChange={setPriority}
        />
      </div>

      {add.error ? <Alert>{add.error.message}</Alert> : null}
    </Modal>
  );
}
