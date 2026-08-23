import { useState } from "react";

import type { ImportSelection, PlannedFeed } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { useImportFeeds, usePreviewImport } from "@app/queries/hooks";

/**
 * Takes somebody else's subscription list.
 *
 * Two steps, because an import is another person's decisions arriving in bulk — which
 * feeds, filed under which names, at which priorities. Applying that unseen is how you end
 * up with a taxonomy you did not choose and cannot easily unpick. So the list is read
 * first and shown as a plan: what you already follow, which of these tags are yours, and
 * which are new. Then you untick whatever you do not want, and only that is sent.
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

  const [text, setText] = useState("");
  const [skipped, setSkipped] = useState<Set<string>>(new Set());
  const [droppedTags, setDroppedTags] = useState<Set<string>>(new Set());

  const plan = preview.data?.feeds;

  function reset() {
    setText("");
    setSkipped(new Set());
    setDroppedTags(new Set());
    preview.reset();
    run.reset();
  }

  function close() {
    reset();
    onClose();
  }

  function read() {
    setSkipped(new Set());
    setDroppedTags(new Set());
    run.reset();
    preview.mutate(text, {
      onSuccess: ({ feeds }) => {
        // Anything already followed starts unticked: importing it again would do nothing,
        // and leaving it ticked would make the count of what happened misleading.
        setSkipped(
          new Set(
            feeds.filter((f) => f.already_subscribed).map((f) => f.feed_url),
          ),
        );
      },
    });
  }

  function keep(feed: PlannedFeed): ImportSelection {
    return {
      feed_url: feed.feed_url,
      title: feed.title,
      site_url: feed.site_url,
      priority: feed.priority,
      tag_paths: feed.tags
        .filter((tag) => !droppedTags.has(tag.name))
        .map((tag) => tag.path),
    };
  }

  const chosen = (plan ?? []).filter((feed) => !skipped.has(feed.feed_url));

  // Every distinct tag the file mentions, so a whole tag can be refused in one place
  // rather than feed by feed.
  const mentioned = new Map<string, boolean>();
  for (const feed of plan ?? []) {
    for (const tag of feed.tags) {
      mentioned.set(tag.name, tag.existing);
    }
  }

  return (
    <Modal open={open} onClose={close} title="Import a list">
      {run.data ? (
        <>
          <p className="text-sm text-ink">
            Added {run.data.added} feed{run.data.added === 1 ? "" : "s"}
            {run.data.skipped > 0
              ? `, skipped ${run.data.skipped} you already follow`
              : ""}
            .
          </p>
          {run.data.tags_created.length > 0 ? (
            <p className="text-xs text-ink-muted">
              New tags: {run.data.tags_created.join(", ")}
            </p>
          ) : null}
          {run.data.failed.length > 0 ? (
            <Alert>
              {run.data.failed.length} could not be added:{" "}
              {run.data.failed.map((f) => f.feed_url).join(", ")}
            </Alert>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button onClick={reset}>Import another</Button>
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          </div>
        </>
      ) : !plan ? (
        <>
          <p className="text-sm text-ink-muted">
            Paste an OPML list — from here or from any other reader. Nothing is
            added until you have seen what it would do.
          </p>
          <textarea
            value={text}
            onChange={(event) => setText(event.target.value)}
            rows={8}
            placeholder='<opml version="2.0">…'
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
          <p className="text-sm text-ink-muted">
            {chosen.length} of {plan.length} will be added. Untick anything you
            do not want.
          </p>

          {mentioned.size > 0 ? (
            <div className="flex flex-col gap-1">
              <p className="text-xs tracking-wide text-ink-faint uppercase">
                Tags in this list
              </p>
              <div className="flex flex-wrap gap-1.5">
                {[...mentioned].map(([name, existing]) => {
                  const dropped = droppedTags.has(name);
                  return (
                    <button
                      key={name}
                      type="button"
                      onClick={() =>
                        setDroppedTags((was) => {
                          const next = new Set(was);
                          if (dropped) next.delete(name);
                          else next.add(name);
                          return next;
                        })
                      }
                      // Yours and new look different on purpose: nobody should be
                      // surprised by a taxonomy appearing in their account.
                      className={`rounded-full border px-2.5 py-1 text-xs ${
                        dropped
                          ? "border-rule text-ink-faint line-through"
                          : existing
                            ? "border-accent bg-accent/10 text-accent"
                            : "border-dashed border-ink-faint text-ink-muted"
                      }`}
                      title={
                        existing ? "one of yours" : "new — will be created"
                      }
                    >
                      {name}
                      {existing ? "" : " +"}
                    </button>
                  );
                })}
              </div>
              <p className="text-xs text-ink-faint">
                Solid are tags you already have; dashed would be created. Click
                to leave one out.
              </p>
            </div>
          ) : null}

          <ul className="max-h-56 overflow-y-auto rounded-md border border-rule">
            {plan.map((feed) => (
              <li
                key={feed.feed_url}
                className="border-b border-rule last:border-b-0"
              >
                <label className="flex cursor-pointer items-baseline gap-2 px-3 py-2 text-sm">
                  <input
                    type="checkbox"
                    checked={!skipped.has(feed.feed_url)}
                    onChange={(event) =>
                      setSkipped((was) => {
                        const next = new Set(was);
                        if (event.target.checked) next.delete(feed.feed_url);
                        else next.add(feed.feed_url);
                        return next;
                      })
                    }
                  />
                  <span className="min-w-0">
                    <span className="block truncate text-ink">
                      {feed.title}
                    </span>
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
              </li>
            ))}
          </ul>

          {run.error ? <Alert>{run.error.message}</Alert> : null}

          <div className="flex justify-end gap-2">
            <Button onClick={reset}>Back</Button>
            <Button
              variant="primary"
              disabled={chosen.length === 0 || run.isPending}
              onClick={() => run.mutate(chosen.map(keep))}
            >
              {run.isPending ? "Adding…" : `Add ${chosen.length}`}
            </Button>
          </div>
        </>
      )}
    </Modal>
  );
}
