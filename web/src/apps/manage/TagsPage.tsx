import { useState } from "react";

import type { Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Priority } from "@app/components/ui/Priority";
import { Select } from "@app/components/ui/Select";
import { Spinner } from "@app/components/ui/Spinner";
import { DEFAULT_PRIORITY } from "@app/lib/constants";
import { useRemoveTag, useTags, useUpdateTag } from "@app/queries/hooks";

import { NewTagDialog } from "@app/apps/manage/NewTagDialog";

export function TagsPage() {
  const tags = useTags();
  const [making, setMaking] = useState(false);

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

      <section className="flex flex-col">
        {tags.data.length === 0 ? (
          <p className="py-10 text-center text-sm text-ink-muted">
            No tags yet. Feeds without one share a bucket at {DEFAULT_PRIORITY},
            which is perfectly workable until you want some things to appear
            more than others.
          </p>
        ) : (
          roots.map((tag) => (
            <div key={tag.id}>
              <TagRow tag={tag} tags={tags.data} />
              {childrenOf(tag.id).map((child) => (
                <TagRow key={child.id} tag={child} tags={tags.data} nested />
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
}: {
  tag: Tag;
  tags: Tag[];
  nested?: boolean;
}) {
  // Counted for the confirmation, which has to say what happens to them — they are promoted
  // rather than deleted, and that is not what "delete" leads anybody to expect.
  const children = tags.filter((other) => other.parent_id === tag.id).length;
  const update = useUpdateTag();
  const remove = useRemoveTag();
  const [name, setName] = useState(tag.name);
  // Whether Delete has been pressed once and not yet answered. Inline on the row rather than
  // a dialog: what is being deleted is named right there, and a box asking about a thing
  // already on screen is a box that says nothing the row does not.
  const [confirming, setConfirming] = useState(false);

  // Only tags that are not this one and are not already nested under something can be a
  // parent. The server refuses a cycle outright; this keeps the obvious ones off the menu.
  const candidates = tags.filter(
    (other) => other.id !== tag.id && other.parent_id === null,
  );

  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-rule py-3">
      {/* The indent is inside the name's box, not on the row.
      
          On the row it pushed every control after it across, so a nested tag's menu and
          slider sat two dozen pixels right of the ones above — a list of five that lined up
          and a sixth that did not. The box is the same width either way, so what a nested
          tag gets is a shorter field, which is what an indent looks like. */}
      {/* mr-auto, so the slack falls here and the controls sit together against the right
          edge. It used to fall between the slider and Delete, which left the one destructive
          thing on the row stranded on its own with a hand's width of nothing beside it. */}
      <div className={`mr-auto w-44 shrink-0 ${nested ? "pl-6" : ""}`}>
        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          onBlur={() => {
            if (name.trim() !== "" && name !== tag.name) {
              update.mutate({ id: tag.id, changes: { name } });
            }
          }}
          aria-label={`Name of ${tag.name}`}
          className="w-full rounded-md border border-transparent bg-transparent px-1.5 py-1
            font-serif text-lg text-ink hover:border-rule focus-visible:border-rule
            focus-visible:outline-none"
        />
      </div>

      {/* Sized here rather than left to the browser, which takes a select's width from its
          widest option — and each of these lists every root tag *except its own*, so the
          widest option differed per row and no two menus were the same width. Three pixels
          on one row, and everything to the right of it out of column. */}
      <Select
        small
        className="w-36 shrink-0"
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

      <Priority
        label={`How often ${tag.name} appears`}
        value={tag.priority}
        onChange={(priority) =>
          update.mutate({ id: tag.id, changes: { priority } })
        }
      />

      {/* A box of its own width, holding one word. What the confirmation needs — two
          buttons and a sentence — goes on the line below instead, because putting it here
          made this cell the widest thing on the row: everything is pushed right, so the last
          cell growing dragged the menu and the slider left on that row alone, and the four
          fixed widths together no longer fitted the page at all. */}
      <div className="flex w-12 shrink-0 justify-end text-xs">
        {confirming ? null : (
          <button
            type="button"
            onClick={() => setConfirming(true)}
            className="text-ink-faint hover:text-accent"
          >
            Delete
          </button>
        )}
      </div>

      {/* What goes with it, said before rather than discovered after. None of it is obvious:
          a tag nested under this one is promoted rather than deleted, the feeds filed here
          keep everything but the filing, and any page that had a rule about this tag quietly
          loses it. */}
      {confirming ? (
        <div className="flex w-full flex-wrap items-baseline justify-end gap-x-4 gap-y-2">
          <p className="min-w-0 flex-1 text-xs text-ink-muted">
            Deleting <span className="text-ink">{tag.name}</span> unfiles every
            feed under it and drops it from any page that had a rule about it.
            The feeds and the pages stay.
            {children === 0
              ? ""
              : children === 1
                ? " The tag nested under it becomes one of its own."
                : ` The ${children} tags nested under it become tags of their own.`}
          </p>
          <span className="flex shrink-0 items-center gap-3 text-xs">
            <button
              type="button"
              onClick={() => setConfirming(false)}
              className="text-ink-faint hover:text-ink"
            >
              Keep
            </button>
            <button
              type="button"
              disabled={remove.isPending}
              onClick={() => remove.mutate(tag.id)}
              className="text-accent disabled:opacity-50"
            >
              {remove.isPending ? "Deleting…" : "Delete it"}
            </button>
          </span>
        </div>
      ) : null}

      {/* Both on their own line, because a refusal is about the row rather than about any
          one control on it — renaming and deleting fail for different reasons and neither
          has room beside the thing that caused it. */}
      {update.error ? (
        <div className="w-full">
          <Alert>{update.error.message}</Alert>
        </div>
      ) : null}
      {remove.error ? (
        <div className="w-full">
          <Alert>{remove.error.message}</Alert>
        </div>
      ) : null}
    </div>
  );
}
