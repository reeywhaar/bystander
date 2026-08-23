import { useState } from "react";

import type { ImportSelection, PlannedFeed, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { tagLabel, tagPath } from "@app/lib/tags";
import { useImportFeeds, usePreviewImport, useTags } from "@app/queries/hooks";

/** A tag chip is identified by the tag it names, whether or not that tag exists yet. */
const ownKey = (id: string) => "id:" + id;
const newKey = (path: string[]) => "new:" + path.join("/");

/**
 * Takes somebody else's subscription list.
 *
 * Two steps, because an import is another person's decisions arriving in bulk — which
 * feeds, filed under which names, at which priorities. Applying that unseen is how you end
 * up with a taxonomy you did not choose and cannot easily unpick. So the list is read
 * first and shown as a plan, and only what is ticked is sent.
 */
export function ImportDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const preview = usePreviewImport();
  const run = useImportFeeds();
  const tags = useTags();

  const [text, setText] = useState("");
  const [skipped, setSkipped] = useState<Set<string>>(new Set());
  // Which tags each feed should arrive with. Per feed rather than per file, because a
  // shared list is one person's filing and it rarely maps onto another's whole.
  const [chosen, setChosen] = useState<Map<string, Set<string>>>(new Map());

  const plan = preview.data?.feeds;
  const mine = tags.data ?? [];

  function reset() {
    setText("");
    setSkipped(new Set());
    setChosen(new Map());
    preview.reset();
    run.reset();
  }

  function close() {
    reset();
    onClose();
  }

  function read() {
    run.reset();
    preview.mutate(text, {
      onSuccess: ({ feeds }) => {
        // Anything already followed starts unticked: importing it again would do nothing.
        setSkipped(
          new Set(
            feeds.filter((f) => f.already_subscribed).map((f) => f.feed_url),
          ),
        );

        // Tags the list named that you already have are ticked — that is a match, not a
        // decision. Tags you do not have start unticked: a taxonomy should arrive because
        // somebody asked for it, not because it came in the post.
        setChosen(
          new Map(
            feeds.map((feed) => [
              feed.feed_url,
              new Set(
                feed.tags.filter((t) => t.tag_id).map((t) => ownKey(t.tag_id)),
              ),
            ]),
          ),
        );
      },
    });
  }

  function toggleTag(feedURL: string, key: string) {
    setChosen((was) => {
      const next = new Map(was);
      const forFeed = new Set(next.get(feedURL) ?? []);
      if (forFeed.has(key)) forFeed.delete(key);
      else forFeed.add(key);
      next.set(feedURL, forFeed);
      return next;
    });
  }

  function selection(feed: PlannedFeed): ImportSelection {
    const keys = chosen.get(feed.feed_url) ?? new Set<string>();
    const paths: string[][] = [];

    for (const key of keys) {
      if (key.startsWith("id:")) {
        const path = tagPath(mine, key.slice(3));
        if (path.length > 0) paths.push(path);
      } else {
        paths.push(key.slice(4).split("/"));
      }
    }
    return {
      feed_url: feed.feed_url,
      title: feed.title,
      site_url: feed.site_url,
      priority: feed.priority,
      tag_paths: paths,
    };
  }

  const keeping = (plan ?? []).filter((feed) => !skipped.has(feed.feed_url));

  return (
    <Modal open={open} onClose={close} title="Import a list">
      {run.data ? (
        <Done result={run.data} onAgain={reset} onClose={close} />
      ) : !plan ? (
        <>
          <p className="text-sm text-ink-muted">
            Paste a list — the OPML kind, or the plain one this hands out.
            Nothing is added until you have seen what it would do.
          </p>
          <textarea
            value={text}
            onChange={(event) => setText(event.target.value)}
            rows={8}
            placeholder={
              "The Go Blog\nhttps://go.dev/blog/feed.atom\nEngineering"
            }
            aria-label="The list to import"
            className="w-full resize-y rounded-md border border-rule bg-paper-sunken p-2
              font-mono text-xs text-ink placeholder:text-ink-faint"
          />
          {preview.error ? <Alert>{preview.error.message}</Alert> : null}
          <div className="flex justify-end gap-2">
            <Button onClick={close}>Cancel</Button>
            <Button
              variant="primary"
              onClick={read}
              disabled={text.trim() === "" || preview.isPending}
            >
              {preview.isPending ? "Reading…" : "Read it"}
            </Button>
          </div>
        </>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              onClick={() => setSkipped(new Set())}
              disabled={keeping.length === plan.length}
            >
              All
            </Button>
            <Button
              onClick={() =>
                setSkipped(new Set(plan.map((feed) => feed.feed_url)))
              }
              disabled={keeping.length === 0}
            >
              None
            </Button>
            <span className="ml-auto text-xs text-ink-faint">
              {keeping.length} of {plan.length}
            </span>
          </div>

          <ul className="max-h-72 overflow-y-auto rounded-md border border-rule">
            {plan.map((feed) => (
              <PlanRow
                key={feed.feed_url}
                feed={feed}
                tags={mine}
                keep={!skipped.has(feed.feed_url)}
                chosen={chosen.get(feed.feed_url) ?? new Set()}
                onKeep={(keep) =>
                  setSkipped((was) => {
                    const next = new Set(was);
                    if (keep) next.delete(feed.feed_url);
                    else next.add(feed.feed_url);
                    return next;
                  })
                }
                onToggleTag={(key) => toggleTag(feed.feed_url, key)}
              />
            ))}
          </ul>

          <p className="text-xs text-ink-faint">
            Solid chips are your own tags — the ones the list named are already
            ticked. Dashed ones are new and would be created.
          </p>

          {run.error ? <Alert>{run.error.message}</Alert> : null}

          <div className="flex justify-end gap-2">
            <Button onClick={reset}>Back</Button>
            <Button
              variant="primary"
              disabled={keeping.length === 0 || run.isPending}
              onClick={() => run.mutate(keeping.map(selection))}
            >
              {run.isPending ? "Adding…" : "Add " + keeping.length}
            </Button>
          </div>
        </>
      )}
    </Modal>
  );
}

/**
 * One feed, with every tag you have underneath it.
 *
 * All of them, not just the ones the list mentioned, because filing a stranger's feed is
 * the moment you actually know where it belongs — and the alternative is importing it and
 * then going to find it again.
 */
function PlanRow({
  feed,
  tags,
  keep,
  chosen,
  onKeep,
  onToggleTag,
}: {
  feed: PlannedFeed;
  tags: Tag[];
  keep: boolean;
  chosen: Set<string>;
  onKeep: (keep: boolean) => void;
  onToggleTag: (key: string) => void;
}) {
  // Tags the list named that nobody here has yet.
  const incoming = feed.tags.filter((tag) => !tag.tag_id);

  return (
    <li className="border-b border-rule px-3 py-2.5 last:border-b-0">
      <label className="flex cursor-pointer items-baseline gap-2 text-sm">
        <input
          type="checkbox"
          checked={keep}
          onChange={(event) => onKeep(event.target.checked)}
        />
        <span className="min-w-0">
          <span className="block truncate text-ink">{feed.title}</span>
          <span className="block truncate text-xs text-ink-faint">
            {feed.feed_url}
          </span>
        </span>
        {feed.already_subscribed ? (
          <span className="ml-auto shrink-0 text-xs text-ink-faint">
            already yours
          </span>
        ) : null}
      </label>

      {keep ? (
        <div className="mt-2 flex flex-wrap items-center gap-1.5 pl-6">
          {tags.map((tag) => (
            <Chip
              key={tag.id}
              label={tagLabel(tags, tag.id)}
              on={chosen.has(ownKey(tag.id))}
              onClick={() => onToggleTag(ownKey(tag.id))}
            />
          ))}

          {incoming.length > 0 ? (
            <>
              {/* The gap is the point: what is already yours and what would be created are
                  different kinds of thing, and running them together is how somebody
                  acquires a taxonomy without noticing. */}
              <span className="mx-2 h-4 w-px bg-rule" aria-hidden="true" />
              {incoming.map((tag) => (
                <Chip
                  key={newKey(tag.path)}
                  label={tag.name}
                  isNew
                  on={chosen.has(newKey(tag.path))}
                  onClick={() => onToggleTag(newKey(tag.path))}
                />
              ))}
            </>
          ) : null}

          {tags.length === 0 && incoming.length === 0 ? (
            <span className="text-xs text-ink-faint">no tags</span>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}

function Chip({
  label,
  on,
  isNew,
  onClick,
}: {
  label: string;
  on: boolean;
  isNew?: boolean;
  onClick: () => void;
}) {
  const style = on
    ? isNew
      ? "border-dashed border-accent bg-accent/10 text-accent"
      : "border-accent bg-accent/10 text-accent"
    : isNew
      ? "border-dashed border-ink-faint text-ink-muted hover:text-ink"
      : "border-rule text-ink-muted hover:text-ink";

  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={on}
      title={isNew ? "new — would be created" : "one of yours"}
      className={"rounded-full border px-2.5 py-0.5 text-xs " + style}
    >
      {label}
      {isNew ? " +" : ""}
    </button>
  );
}

function Done({
  result,
  onAgain,
  onClose,
}: {
  result: {
    added: number;
    skipped: number;
    failed: { feed_url: string }[];
    tags_created: string[];
  };
  onAgain: () => void;
  onClose: () => void;
}) {
  return (
    <>
      <p className="text-sm text-ink">
        Added {result.added} feed{result.added === 1 ? "" : "s"}
        {result.skipped > 0
          ? ", skipped " + result.skipped + " you already follow"
          : ""}
        .
      </p>
      {result.tags_created.length > 0 ? (
        <p className="text-xs text-ink-muted">
          New tags: {result.tags_created.join(", ")}
        </p>
      ) : null}
      {result.failed.length > 0 ? (
        <Alert>
          {result.failed.length} could not be added:{" "}
          {result.failed.map((f) => f.feed_url).join(", ")}
        </Alert>
      ) : null}
      <div className="flex justify-end gap-2">
        <Button onClick={onAgain}>Import another</Button>
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </div>
    </>
  );
}
