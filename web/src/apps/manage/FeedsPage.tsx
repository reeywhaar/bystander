import { useState, type FormEvent } from "react";

import type { PlannedFeed, Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Reach } from "@app/components/ui/Reach";
import { Modal } from "@app/components/ui/Modal";

import {
  FeedPlan,
  initialSelection,
  kept,
  offered,
  toImport,
  type PlanSelection,
} from "@app/apps/manage/FeedPlan";
import { FeedDialog } from "@app/apps/manage/FeedDialog";
import { FeedErrorDialog } from "@app/apps/manage/FeedErrorDialog";
import { ImportDialog } from "@app/apps/manage/ImportDialog";
import { ShareDialog } from "@app/apps/manage/ShareDialog";
import { Priority } from "@app/components/ui/Priority";
import { Spinner } from "@app/components/ui/Spinner";
import { tagLabel } from "@app/lib/tags";
import { since } from "@app/lib/time";
import {
  useDiscoverFeeds,
  useFeeds,
  useImportFeeds,
  useTags,
  useUpdateFeed,
} from "@app/queries/hooks";

export function FeedsPage() {
  const feeds = useFeeds();
  const tags = useTags();
  const discover = useDiscoverFeeds();
  const add = useImportFeeds();

  const [url, setUrl] = useState("");
  // What the site turned out to offer, once there is more than one thing to choose from.
  const [choices, setChoices] = useState<PlannedFeed[] | null>(null);
  const [selection, setSelection] = useState<PlanSelection>(
    initialSelection([]),
  );
  // Anything that stopped the address becoming a subscription. A dialog rather than a line
  // of text under the field, because this is the end of the attempt and not a hint about
  // it — the address needs changing, or the site has no feed at all.
  const [problem, setProblem] = useState<string | null>(null);
  const [sharing, setSharing] = useState(false);
  const [importing, setImporting] = useState(false);
  // The id, not the feed. Holding the object would hold a snapshot taken when the dialog
  // opened: renaming or retagging from inside it would update the list underneath and
  // leave the dialog still describing what used to be true.
  const [editingID, setEditingID] = useState<string | null>(null);

  function subscribe(feed: PlannedFeed) {
    // One feed and no choice to make: straight in, untagged, as it always was. The picker
    // is for when a site offers several — see below.
    add.mutate(toImport([feed], initialSelection([feed]), tags.data ?? []), {
      onSuccess: () => {
        setUrl("");
        setChoices(null);
      },
      onError: (error) => {
        setChoices(null);
        setProblem(error.message);
      },
    });
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    setProblem(null);

    // Ask what the address is before subscribing to it. A site names its feeds in the
    // markup and usually names several — posts, comments, a podcast — and picking the
    // first is how somebody ends up following comments they never wanted.
    discover.mutate(url, {
      onSuccess: ({ candidates }) => {
        // Counted after dropping what is already followed, so a site whose other feed you
        // took last week still goes straight in rather than opening a picker with one row.
        const fresh = offered(candidates);
        if (fresh.length === 1 && fresh[0]) {
          subscribe(fresh[0]);
        } else {
          setSelection(initialSelection(candidates));
          setChoices(candidates);
        }
      },
      onError: (error) => setProblem(error.message),
    });
  }

  const working = discover.isPending || add.isPending;

  if (feeds.isPending || tags.isPending) return <Spinner />;
  if (feeds.error) throw feeds.error;
  if (tags.error) throw tags.error;

  return (
    <div className="flex flex-col gap-8">
      <section>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <div className="flex gap-2">
            <input
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              required
              placeholder="example.com, or a feed URL"
              aria-label="Feed or site address"
              className="flex-1 rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm
                placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-accent"
            />
            <Button type="submit" variant="primary" disabled={working}>
              {discover.isPending
                ? "Looking…"
                : add.isPending
                  ? "Adding…"
                  : "Add"}
            </Button>
          </div>
          <p className="text-xs text-ink-muted">
            A site's address is enough — bystander looks for the feeds it offers
            and asks which you want.
          </p>
        </form>

        {/* Beside adding one, because they are the same job at a different scale: getting
            feeds in, and handing them on. */}
        <div className="mt-3 flex flex-wrap gap-2">
          <Button onClick={() => setImporting(true)}>Import a list</Button>
          <Button
            onClick={() => setSharing(true)}
            disabled={feeds.data.length === 0}
          >
            Share my feeds
          </Button>
        </div>
      </section>

      <ShareDialog
        open={sharing}
        onClose={() => setSharing(false)}
        feeds={feeds.data}
        tags={tags.data}
      />
      <ImportDialog open={importing} onClose={() => setImporting(false)} />
      <FeedDialog
        feed={feeds.data.find((feed) => feed.id === editingID) ?? null}
        tags={tags.data ?? []}
        onClose={() => setEditingID(null)}
      />

      <Modal
        open={choices !== null}
        onClose={() => setChoices(null)}
        title="Which of these?"
        footer={
          choices ? (
            <>
              <Button onClick={() => setChoices(null)}>Cancel</Button>
              <Button
                variant="primary"
                disabled={
                  kept(choices, selection).length === 0 || add.isPending
                }
                onClick={() =>
                  add.mutate(toImport(choices, selection, tags.data ?? []), {
                    onSuccess: () => {
                      setUrl("");
                      setChoices(null);
                    },
                  })
                }
              >
                {add.isPending
                  ? "Adding…"
                  : "Add " + kept(choices, selection).length}
              </Button>
            </>
          ) : null
        }
      >
        {choices ? (
          <>
            <p className="text-sm text-ink-muted">
              That site offers {choices.length} feeds. Take as many as you like.
            </p>

            <FeedPlan
              feeds={choices}
              tags={tags.data ?? []}
              selection={selection}
              onChange={setSelection}
            />

            {add.error ? <Alert>{add.error.message}</Alert> : null}
          </>
        ) : null}
      </Modal>

      <Modal
        open={problem !== null}
        onClose={() => setProblem(null)}
        title="That did not work"
        footer={
          <Button variant="primary" onClick={() => setProblem(null)}>
            Close
          </Button>
        }
      >
        <p className="text-sm text-ink-muted">{problem}</p>
      </Modal>

      <section className="flex flex-col gap-1">
        {feeds.data.length === 0 ? (
          <p className="py-10 text-center text-sm text-ink-muted">
            No feeds yet. Add one above, and a page will be composed from it.
          </p>
        ) : (
          feeds.data.map((feed) => (
            <FeedRow
              key={feed.id}
              feed={feed}
              tags={tags.data}
              onOpen={(feed) => setEditingID(feed.id)}
            />
          ))
        )}
      </section>
    </div>
  );
}

function FeedRow({
  feed,
  tags,
  onOpen,
}: {
  feed: Subscription;
  tags: Tag[];
  onOpen: (feed: Subscription) => void;
}) {
  const update = useUpdateFeed();
  const [explaining, setExplaining] = useState(false);

  const failing = feed.failure_count > 0;
  // Full paths, so a nested tag reads as "News / World" rather than losing where it sits.
  const labels = feed.tag_ids.map((id) => tagLabel(tags, id)).filter(Boolean);

  return (
    <div className="border-b border-rule py-3">
      {/* Side by side on a wide screen, stacked on a narrow one.

          The priority control is a fixed ~16rem — a label that must not resize plus a
          track — which on a phone leaves nothing for the name, and `truncate` duly
          truncated it to nothing.

          So on a narrow screen it is three lines: the name, then where it is filed, then
          the slider. `order` rather than a second copy of the markup, because the slider
          belongs beside the name on a wide screen and under everything on a narrow one. */}
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <div className="order-1 flex min-w-0 basis-full items-baseline gap-x-2 sm:flex-1 sm:basis-auto">
          {/* The name is the way into everything else about this feed. One affordance
              rather than a pencil for the title and a disclosure for the rest. */}
          <button
            type="button"
            onClick={() => onOpen(feed)}
            title={feed.title}
            className="min-w-0 truncate text-left font-serif text-lg text-ink hover:text-accent"
          >
            {feed.title}
          </button>
        </div>

        {/* A quieter line for what the name has no room for: where this feed is filed,
            how long it has been here, and whether it is answering. */}
        <p className="order-2 basis-full text-xs break-words text-ink-faint sm:order-3">
          {labels.length > 0 ? (
            <span className="text-ink-muted">{labels.join(" · ")}</span>
          ) : null}
          {labels.length > 0 ? " · " : ""}
          {/* Everything the dialog holds is said here too, so opening one tells you
              nothing the list was keeping back. */}
          <Reach seconds={feed.article_window} /> · added{" "}
          {since(feed.created_at)}
          {failing ? (
            <>
              {" · "}
              {/* A button, because this was a `title` tooltip: it needed a pointer to
                  hover, so on a phone the answer to "why is this not answering" could not
                  be reached at all. */}
              <button
                type="button"
                onClick={() => setExplaining(true)}
                className="text-accent underline decoration-dotted underline-offset-2"
              >
                not answering ({feed.failure_count})
              </button>
            </>
          ) : feed.last_success_at ? (
            <> · fetched {since(feed.last_success_at)}</>
          ) : (
            <> · not fetched yet</>
          )}
        </p>

        {/* The one setting that stays in the list: it is a dial somebody nudges while
            looking at the whole of it, not something they go and open a feed to change. */}
        <div className="order-3 shrink-0 sm:order-2 sm:ml-auto">
          <Priority
            label={`How often ${feed.title} appears`}
            value={feed.priority}
            onChange={(priority) =>
              update.mutate({ id: feed.id, changes: { priority } })
            }
          />
        </div>
      </div>

      {/* Mounted only while it is open, so a page of thirty feeds is not thirty dialogs. */}
      {explaining ? (
        <FeedErrorDialog
          feed={feed}
          open={explaining}
          onClose={() => setExplaining(false)}
        />
      ) : null}
    </div>
  );
}
