import { useState, type FormEvent } from "react";

import type { PlannedFeed, Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";

import {
  FeedPlan,
  initialSelection,
  kept,
  offered,
  toImport,
  type PlanSelection,
} from "@app/apps/manage/FeedPlan";
import { ImportDialog } from "@app/apps/manage/ImportDialog";
import { RenameDialog } from "@app/apps/manage/RenameDialog";
import { ShareDialog } from "@app/apps/manage/ShareDialog";
import { PencilIcon } from "@app/components/ui/icons/PencilIcon";
import { Priority } from "@app/components/ui/Priority";
import { Spinner } from "@app/components/ui/Spinner";
import { tagLabel } from "@app/lib/tags";
import { since } from "@app/lib/time";
import {
  useDiscoverFeeds,
  useFeeds,
  useImportFeeds,
  useRemoveFeed,
  useTags,
  useUpdateFeed,
} from "@app/queries/hooks";

export function FeedsPage() {
  const feeds = useFeeds();
  const tags = useTags();
  const discover = useDiscoverFeeds();
  const add = useImportFeeds();
  const rename = useUpdateFeed();

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
  const [renaming, setRenaming] = useState<Subscription | null>(null);

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
      <RenameDialog
        feed={renaming}
        saving={rename.isPending}
        onClose={() => setRenaming(null)}
        onSave={(title) =>
          rename.mutate(
            { id: renaming?.id ?? "", changes: { title_override: title } },
            { onSuccess: () => setRenaming(null) },
          )
        }
      />

      <Modal
        open={choices !== null}
        onClose={() => setChoices(null)}
        title="Which of these?"
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

            <div className="flex justify-end gap-2">
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
            </div>
          </>
        ) : null}
      </Modal>

      <Modal
        open={problem !== null}
        onClose={() => setProblem(null)}
        title="That did not work"
      >
        <p className="text-sm text-ink-muted">{problem}</p>
        <div className="flex justify-end">
          <Button variant="primary" onClick={() => setProblem(null)}>
            Close
          </Button>
        </div>
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
              onRename={setRenaming}
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
  onRename,
}: {
  feed: Subscription;
  tags: Tag[];
  onRename: (feed: Subscription) => void;
}) {
  const update = useUpdateFeed();
  const remove = useRemoveFeed();
  const [open, setOpen] = useState(false);

  const failing = feed.failure_count > 0;
  // Full paths, so a nested tag reads as "News / World" rather than losing where it sits.
  const labels = feed.tag_ids.map((id) => tagLabel(tags, id)).filter(Boolean);

  return (
    <div className="border-b border-rule py-3">
      {/* Two columns, one line each. The name gives way first — trimmed with an ellipsis
          and carrying the whole of itself in a title — so a long one makes the row no
          taller and never shoves the slider onto a line of its own, which is what turned
          this list into a staircase. */}
      {/* Side by side on a wide screen, stacked on a narrow one.
          The priority control is a fixed ~16rem — a label that must not resize plus a
          track — which on a phone leaves nothing for the name, and `truncate` duly
          truncated it to nothing.

          So on a narrow screen it is three lines: the name, then where it is filed, then
          the slider. `order` rather than a second copy of the markup, because the slider
          belongs beside the name on a wide screen and under everything on a narrow one. */}
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <div className="order-1 flex min-w-0 basis-full items-baseline gap-x-2 sm:flex-1 sm:basis-auto">
          <button
            type="button"
            onClick={() => setOpen((was) => !was)}
            aria-expanded={open}
            title={feed.title}
            className="min-w-0 truncate text-left font-serif text-lg text-ink hover:text-accent"
          >
            {feed.title}
          </button>

          {/* Beside the name, because that is what it renames. Its own button rather than
              part of the toggle, so opening a row and renaming it stay separate gestures. */}
          <button
            type="button"
            onClick={() => onRename(feed)}
            aria-label={`Rename ${feed.title}`}
            title="Rename"
            className="shrink-0 text-ink-faint hover:text-ink"
          >
            <PencilIcon />
          </button>
          {failing ? (
            <span
              className="shrink-0 text-xs text-accent"
              title={feed.last_error}
            >
              not answering ({feed.failure_count})
            </span>
          ) : feed.last_success_at ? (
            <span className="shrink-0 text-xs text-ink-faint">
              {since(feed.last_success_at)}
            </span>
          ) : (
            <span className="shrink-0 text-xs text-ink-faint">
              not fetched yet
            </span>
          )}
        </div>

        {/* A quieter line for what the name has no room for: where this feed is filed,
            and how long it has been here. The tags drop away when the row is open,
            because the chips below are the same information and can be acted on. */}
        <p className="order-2 basis-full text-xs break-words text-ink-faint sm:order-3">
          {!open && labels.length > 0 ? (
            <span className="text-ink-muted">{labels.join(" · ")}</span>
          ) : null}
          {!open && labels.length > 0 ? " · " : ""}
          added {since(feed.created_at)}
        </p>

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

      {open ? (
        <div className="mt-3 flex flex-col gap-3 pl-1">
          <p className="text-xs break-all text-ink-faint">{feed.url}</p>

          {failing ? <Alert>{feed.last_error}</Alert> : null}

          <div className="flex flex-wrap items-center gap-2">
            {tags.length === 0 ? (
              <p className="text-xs text-ink-muted">
                No tags yet. Tags are how you say which kinds of thing appear
                more often.
              </p>
            ) : (
              tags.map((tag) => {
                const on = feed.tag_ids.includes(tag.id);
                return (
                  <button
                    key={tag.id}
                    type="button"
                    onClick={() =>
                      update.mutate({
                        id: feed.id,
                        changes: {
                          tag_ids: on
                            ? feed.tag_ids.filter((id) => id !== tag.id)
                            : [...feed.tag_ids, tag.id],
                        },
                      })
                    }
                    className={`rounded-full border px-2.5 py-1 text-xs ${
                      on
                        ? "border-accent bg-accent/10 text-accent"
                        : "border-rule text-ink-muted hover:text-ink"
                    }`}
                  >
                    {tag.name}
                  </button>
                );
              })
            )}
          </div>

          <div>
            <Button
              variant="danger"
              onClick={() => remove.mutate(feed.id)}
              disabled={remove.isPending}
            >
              Stop following
            </Button>
          </div>
          {update.error ? <Alert>{update.error.message}</Alert> : null}
          {remove.error ? <Alert>{remove.error.message}</Alert> : null}
        </div>
      ) : null}
    </div>
  );
}
