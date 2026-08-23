import { useState, type FormEvent } from "react";

import type { Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Priority } from "@app/components/ui/Priority";
import { Spinner } from "@app/components/ui/Spinner";
import { DEFAULT_PRIORITY } from "@app/lib/constants";
import {
  useAddTag,
  useRemoveTag,
  useTags,
  useUpdateTag,
} from "@app/queries/hooks";

export function TagsPage() {
  const tags = useTags();
  const add = useAddTag();
  const [name, setName] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate({ name }, { onSuccess: () => setName("") });
  }

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
        <form onSubmit={submit} className="flex gap-2">
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            required
            maxLength={48}
            placeholder="World News, Art, Long reads…"
            aria-label="Tag name"
            className="flex-1 rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm
              placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-accent"
          />
          <Button type="submit" variant="primary" disabled={add.isPending}>
            Add tag
          </Button>
        </form>
        {add.error ? (
          <div className="mt-3">
            <Alert>{add.error.message}</Alert>
          </div>
        ) : null}
      </section>

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
  const update = useUpdateTag();
  const remove = useRemoveTag();
  const [name, setName] = useState(tag.name);

  // Only tags that are not this one and are not already nested under something can be a
  // parent. The server refuses a cycle outright; this keeps the obvious ones off the menu.
  const candidates = tags.filter(
    (other) => other.id !== tag.id && other.parent_id === null,
  );

  return (
    <div
      className={`flex flex-wrap items-center gap-3 border-b border-rule py-3 ${nested ? "pl-6" : ""}`}
    >
      <input
        value={name}
        onChange={(event) => setName(event.target.value)}
        onBlur={() => {
          if (name.trim() !== "" && name !== tag.name) {
            update.mutate({ id: tag.id, changes: { name } });
          }
        }}
        aria-label={`Name of ${tag.name}`}
        className="w-44 rounded-md border border-transparent bg-transparent px-1.5 py-1 font-serif
          text-lg text-ink hover:border-rule focus-visible:border-rule focus-visible:outline-none"
      />

      <select
        value={tag.parent_id ?? ""}
        onChange={(event) =>
          update.mutate({
            id: tag.id,
            changes: { parent_id: event.target.value },
          })
        }
        aria-label={`Where ${tag.name} sits`}
        className="rounded-md border border-rule bg-paper-raised px-2 py-1 text-xs text-ink-muted"
      >
        <option value="">on its own</option>
        {candidates.map((candidate) => (
          <option key={candidate.id} value={candidate.id}>
            under {candidate.name}
          </option>
        ))}
      </select>

      <Priority
        label={`How often ${tag.name} appears`}
        value={tag.priority}
        disabled={update.isPending}
        onChange={(priority) =>
          update.mutate({ id: tag.id, changes: { priority } })
        }
      />

      <button
        type="button"
        onClick={() => remove.mutate(tag.id)}
        className="ml-auto text-xs text-ink-faint hover:text-accent"
      >
        Delete
      </button>

      {update.error ? (
        <div className="w-full">
          <Alert>{update.error.message}</Alert>
        </div>
      ) : null}
    </div>
  );
}
