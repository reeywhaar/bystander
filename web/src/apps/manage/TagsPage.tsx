import { useState } from "react";

import type { Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { PriorityField } from "@app/components/ui/PriorityField";
import { Select } from "@app/components/ui/Select";
import { Spinner } from "@app/components/ui/Spinner";
import { DEFAULT_PRIORITY } from "@app/lib/constants";
import { useTags, useUpdateTag } from "@app/queries/hooks";

import { DeleteTagDialog } from "@app/apps/manage/DeleteTagDialog";
import { NewTagDialog } from "@app/apps/manage/NewTagDialog";

export function TagsPage() {
  const tags = useTags();
  const [making, setMaking] = useState(false);
  // Which tag is being deleted, or null. One dialog for the list rather than one per row.
  const [deleting, setDeleting] = useState<Tag | null>(null);

  if (tags.isPending) return <Spinner />;
  if (tags.error) throw tags.error;

  const roots = tags.data.filter((tag) => tag.parent_id === null);
  const childrenOf = (id: string) =>
    tags.data.filter((tag) => tag.parent_id === id);

  return (
    <div className="flex flex-col gap-8">
      <section>
        <p className="mb-3 text-sm text-ink-muted">
          A tag groups feeds and says how often that group appears. Priority is
          a probability, not an order — a tag at 90 shows up more than one at 10
          without ever silencing it, and 0 means never.
        </p>
        {/* A button rather than a field and a button. A tag is three decisions — the name,
            where it sits, how often it appears — and the field could only take the first,
            so every tag arrived on its own at the default and the other two were set
            afterwards from a row somebody had to find again. */}
        <Button variant="primary" onClick={() => setMaking(true)}>
          New tag
        </Button>
      </section>

      <NewTagDialog
        open={making}
        tags={tags.data}
        onClose={() => setMaking(false)}
      />

      <DeleteTagDialog
        tag={deleting}
        tags={tags.data}
        onClose={() => setDeleting(null)}
      />

      {/* Cards on a phone, a ruled table on a screen — see the row itself. The gap is what
          separates them there, so it goes when the rules come back. */}
      <section className="flex flex-col gap-2 sm:gap-0">
        {tags.data.length === 0 ? (
          <p className="py-10 text-center text-sm text-ink-muted">
            No tags yet. Feeds without one share a bucket at {DEFAULT_PRIORITY},
            which is perfectly workable until you want some things to appear
            more than others.
          </p>
        ) : (
          roots.map((tag) => (
            <div key={tag.id} className="flex flex-col gap-2 sm:gap-0">
              <TagRow tag={tag} tags={tags.data} onDelete={setDeleting} />
              {childrenOf(tag.id).map((child) => (
                <TagRow
                  key={child.id}
                  tag={child}
                  tags={tags.data}
                  nested
                  onDelete={setDeleting}
                />
              ))}
            </div>
          ))
        )}
      </section>
    </div>
  );
}

function TagRow({
  tag,
  tags,
  nested = false,
  onDelete,
}: {
  tag: Tag;
  tags: Tag[];
  nested?: boolean;
  /** Raised to the page, which owns the one dialog for the whole list. */
  onDelete: (tag: Tag) => void;
}) {
  const update = useUpdateTag();
  const [name, setName] = useState(tag.name);

  // Only tags that are not this one and are not already nested under something can be a
  // parent. The server refuses a cycle outright; this keeps the obvious ones off the menu.
  const candidates = tags.filter(
    (other) => other.id !== tag.id && other.parent_id === null,
  );

  return (
    // Two lines on a phone, one on a screen. The name is what a row is *for*, so it gets a
    // line of its own and the full width of it there; the three controls travel together
    // underneath. Wide enough, the wrapper below becomes `display: contents` and every one
    // of them rejoins this row as a direct child, which is what puts them back in column.
    //
    // And a block on its own ground rather than a hairline underneath it. A rule can only
    // separate rows that are single lines: stacked, every field in the row already draws a
    // border of its own, and one more hairline among five is not a boundary — the rows run
    // together and the eye has nothing to group by. A card says where one tag ends. On a
    // screen the rows are single lines again, so the rule does the job and costs nothing.
    <div
      className="flex flex-col gap-2 rounded-md bg-paper-sunken p-3
        sm:flex-row sm:flex-wrap sm:items-center sm:gap-3 sm:rounded-none
        sm:border-b sm:border-rule sm:bg-transparent sm:px-0 sm:py-3"
    >
      {/* The indent is inside the name's box, not on the row.
      
          On the row it pushed every control after it across, so a nested tag's menu and
          slider sat two dozen pixels right of the ones above — a list of five that lined up
          and a sixth that did not. The box is the same width either way, so what a nested
          tag gets is a shorter field, which is what an indent looks like.

          `mr-auto` from `sm` up, so the slack falls here and the controls sit together
          against the right edge rather than leaving the one destructive thing on the row
          stranded with a hand's width of nothing beside it. */}
      <div className={`shrink-0 sm:mr-auto sm:w-44 ${nested ? "pl-6" : ""}`}>
        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          onBlur={() => {
            if (name.trim() !== "" && name !== tag.name) {
              update.mutate({ id: tag.id, changes: { name } });
            }
          }}
          aria-label={`Name of ${tag.name}`}
          // Bordered on a phone, and only on hover above it.
          //
          // On a screen the border arrives when the pointer does, and a list of names set in
          // serif reads as a list rather than as a form. A phone has no pointer, so that
          // border never arrives — and a tag's name then looks exactly like a heading over
          // the controls beneath it, which is the one thing on the row it is not. What is
          // shown there is what hovering shows here.
          className="w-full rounded-md border border-rule bg-paper-raised px-1.5 py-1
            font-serif text-lg text-ink hover:border-rule focus-visible:border-rule
            focus-visible:outline-none sm:border-transparent sm:bg-transparent"
        />
      </div>

      {/* One group on a phone, four columns on a screen — see the row above. */}
      <div className="flex flex-wrap items-center gap-3 sm:contents">
        {/* Sized here rather than left to the browser, which takes a select's width from its
          widest option — and each of these lists every root tag *except its own*, so the
          widest option differed per row and no two menus were the same width. Three pixels
          on one row, and everything to the right of it out of column. */}
        <Select
          small
          className="order-1 w-36 shrink-0 sm:order-none"
          value={tag.parent_id ?? ""}
          onChange={(event) =>
            update.mutate({
              id: tag.id,
              changes: { parent_id: event.target.value },
            })
          }
          aria-label={`Where ${tag.name} sits`}
        >
          <option value="">on its own</option>
          {candidates.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              under {candidate.name}
            </option>
          ))}
        </Select>

        {/* The same field the feed list uses — see PriorityField, which carries the
            reasoning. The width is this row's to decide: the whole line on a phone, a column
            of its own on a screen. */}
        <PriorityField
          className="order-3 w-full sm:order-none sm:w-64"
          label={`How often ${tag.name} appears`}
          value={tag.priority}
          onChange={(priority) =>
            update.mutate({ id: tag.id, changes: { priority } })
          }
        />

        <button
          type="button"
          onClick={() => onDelete(tag)}
          // Beside the menu on a phone and pushed to the far end of that line, rather than
          // alone below everything. On a screen it is the row's last column again.
          className="order-2 ml-auto shrink-0 text-xs text-ink-faint hover:text-accent
            sm:order-none sm:ml-0"
        >
          Delete
        </button>
      </div>

      {/* On its own line: a refusal is about the row rather than about the field that
          caused it, and there is no room for it beside one. */}
      {update.error ? (
        <div className="w-full">
          <Alert>{update.error.message}</Alert>
        </div>
      ) : null}
    </div>
  );
}
