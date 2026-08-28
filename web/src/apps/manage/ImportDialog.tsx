import { useState } from "react";

import type { PlannedFeed } from "@app/api/types";

import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { useImportFeeds, usePreviewImport, useTags } from "@app/queries/hooks";

import {
  FeedPlan,
  initialSelection,
  kept,
  ownKey,
  toImport,
  type PlanSelection,
} from "@app/apps/manage/FeedPlan";
import { ImportOutcome } from "@app/apps/manage/ImportOutcome";
import { NewTagDialog } from "@app/apps/manage/NewTagDialog";
import { PreviewDialog } from "@app/apps/manage/PreviewDialog";

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
  const [selection, setSelection] = useState<PlanSelection>(
    initialSelection([]),
  );
  // Whichever row is being looked at. Its Add ticks that row rather than importing it: the
  // list is still there to be finished, and the import happens once at the bottom.
  const [previewing, setPreviewing] = useState<PlannedFeed | null>(null);
  // Which row asked for a tag it does not have, by feed URL — see FeedPlan's onNewTag.
  const [makingTagFor, setMakingTagFor] = useState<string | null>(null);

  const plan = preview.data?.feeds;
  const mine = tags.data ?? [];

  function reset() {
    setText("");
    setPreviewing(null);
    setSelection(initialSelection([]));
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
      onSuccess: ({ feeds }) => setSelection(initialSelection(feeds)),
    });
  }

  const keeping = plan ? kept(plan, selection) : [];

  // One row, three steps. Written out here rather than at the end of each branch, so the
  // arrangement cannot drift between them.
  const footer = run.data ? (
    <>
      <Button onClick={reset}>Import another</Button>
      <Button variant="primary" onClick={close}>
        Done
      </Button>
    </>
  ) : !plan ? (
    <>
      <Button onClick={close}>Cancel</Button>
      <Button
        variant="primary"
        onClick={read}
        disabled={text.trim() === "" || preview.isPending}
      >
        {preview.isPending ? "Reading…" : "Read it"}
      </Button>
    </>
  ) : (
    <>
      <Button onClick={reset}>Back</Button>
      <Button
        variant="primary"
        disabled={keeping.length === 0 || run.isPending}
        onClick={() => run.mutate(toImport(plan, selection, mine))}
      >
        {run.isPending ? "Adding…" : "Add " + keeping.length}
      </Button>
    </>
  );

  return (
    <Modal open={open} onClose={close} title="Import a list" footer={footer}>
      {run.data ? (
        <ImportOutcome result={run.data} />
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
        </>
      ) : (
        <>
          <FeedPlan
            feeds={plan}
            tags={mine}
            selection={selection}
            onChange={setSelection}
            onPreview={setPreviewing}
            onNewTag={setMakingTagFor}
          />

          {run.error ? <Alert>{run.error.message}</Alert> : null}
        </>
      )}
      {makingTagFor !== null ? (
        <NewTagDialog
          open
          tags={mine}
          onClose={() => setMakingTagFor(null)}
          onCreated={(tag) => {
            const tagsFor = new Map(selection.tags);
            const forFeed = new Set(tagsFor.get(makingTagFor) ?? []);
            forFeed.add(ownKey(tag.id));
            tagsFor.set(makingTagFor, forFeed);
            setSelection({ ...selection, tags: tagsFor });
          }}
        />
      ) : null}

      <PreviewDialog
        feed={previewing}
        open={previewing !== null}
        onClose={() => setPreviewing(null)}
        onAdd={() => {
          if (!previewing) return;
          const next = new Set(selection.skipped);
          next.delete(previewing.feed_url);
          setSelection({ ...selection, skipped: next });
          setPreviewing(null);
        }}
      />
    </Modal>
  );
}
