import { useEffect, useState } from "react";
import { useParams } from "react-router";

import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute } from "@app/lib/time";
import { useImportFeeds, useSharedList, useTags } from "@app/queries/hooks";

import {
  FeedPlan,
  initialSelection,
  kept,
  toImport,
  type PlanSelection,
} from "@app/apps/manage/FeedPlan";
import { ImportOutcome } from "@app/apps/manage/ImportOutcome";

/**
 * What somebody else's link holds.
 *
 * The same picker a pasted file gets, because it is the same question: which of these do I
 * want, and filed under what. Nothing here is subscribed to by arriving — a link that added
 * feeds by being opened would be a link nobody could safely follow.
 *
 * A page rather than a dialog, unlike the import. This is where a link *lands*: whoever
 * follows it has just arrived, possibly signing in on the way, and a modal over a page they
 * never asked for would leave them nowhere when they close it.
 */
export function SharePage() {
  const { token = "" } = useParams();
  const shared = useSharedList(token);
  const tags = useTags();
  const run = useImportFeeds();

  const [selection, setSelection] = useState<PlanSelection>({
    skipped: new Set(),
    tags: new Map(),
  });

  const feeds = shared.data?.feeds;
  useEffect(() => {
    if (feeds) setSelection(initialSelection(feeds));
  }, [feeds]);

  if (shared.isPending || tags.isPending) return <Spinner />;

  // Not thrown to the boundary. An expired or mistyped link is the ordinary way this page
  // is reached — messaging apps cut long URLs — and it deserves a sentence, not a crash
  // screen.
  if (shared.error) {
    return (
      <div className="flex flex-col gap-4">
        <Alert>{shared.error.message}</Alert>
        <p className="max-w-prose text-sm text-ink-muted">
          Shared links are good for a week. Ask whoever sent this one for
          another.
        </p>
        <div>
          <Button onClick={() => (window.location.href = "/manage")}>
            Go to your feeds
          </Button>
        </div>
      </div>
    );
  }

  const list = shared.data;
  const keeping = kept(list.feeds, selection);

  if (run.data) {
    return (
      <div className="flex flex-col gap-4">
        <h2 className="font-serif text-xl text-ink">Added</h2>
        <ImportOutcome result={run.data} />
        <div className="flex flex-wrap gap-2">
          <Button
            variant="primary"
            onClick={() => (window.location.href = "/manage")}
          >
            Go to your feeds
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <section className="flex flex-col gap-2">
        <h2 className="font-serif text-xl text-ink">
          {list.from} shared {list.feeds.length} feed
          {list.feeds.length === 1 ? "" : "s"}
        </h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Take as many as you like. Nothing is added until you say so, and this
          link works until {absolute(list.expires_at)}.
        </p>
      </section>

      <FeedPlan
        feeds={list.feeds}
        tags={tags.data ?? []}
        selection={selection}
        onChange={setSelection}
      />

      {run.error ? <Alert>{run.error.message}</Alert> : null}

      <div className="flex flex-wrap gap-2 border-t border-rule pt-4">
        <Button onClick={() => (window.location.href = "/manage")}>
          Not now
        </Button>
        <Button
          variant="primary"
          disabled={keeping.length === 0 || run.isPending}
          onClick={() =>
            run.mutate(toImport(list.feeds, selection, tags.data ?? []))
          }
        >
          {run.isPending ? "Adding…" : "Add " + keeping.length}
        </Button>
      </div>
    </div>
  );
}
