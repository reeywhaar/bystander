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
import { PreviewDialog } from "@app/apps/manage/PreviewDialog";
import { ShareDialog } from "@app/apps/manage/ShareDialog";
import { EyeIcon } from "@app/components/icons/EyeIcon";
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
  // The feed being looked at, from either flow. What its Add means follows from whether the
  // picker is open behind it: on its own it subscribes, and over a list it ticks a row.
  const [previewing, setPreviewing] = useState<PlannedFeed | null>(null);

  function subscribe(feed: PlannedFeed) {
    // One feed and no choice of which: straight in, untagged. The picker is for when a site
    // offers several — see below.
    add.mutate(toImport([feed], initialSelection([feed]), tags.data ?? []), {
      onSuccess: () => {
        setUrl("");
        setChoices(null);
        setPreviewing(null);
      },
      onError: (error) => {
        setChoices(null);
        setPreviewing(null);
        setProblem(error.message);
      },
    });
  }

  /**
   * What the Add at the bottom of a preview does.
   *
   * Two things, because the preview is opened from two places and the person pressing it
   * means the same thing both times — yes, this one. On its own that is a subscription; over
   * a list of several it is a tick, and the list is still there to be finished.
   */
  function addPreviewed() {
    const feed = previewing;
    if (!feed) return;

    if (choices) {
      const next = new Set(selection.skipped);
      next.delete(feed.feed_url);
      setSelection({ ...selection, skipped: next });
      setPreviewing(null);
      return;
    }
    subscribe(feed);
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
          // Shown rather than subscribed. An address is not a description, and this is the
          // moment somebody can still say no cheaply — after it is a subscription, saying no
          // means unfollowing and losing the read marks with it.
          setPreviewing(fresh[0]);
        } else {
          // Nothing ticked. A site that turns out to offer five feeds chose none of them,
          // and a screen that starts by assuming all five makes "None" the first thing
          // anybody has to press.
          setSelection(initialSelection(candidates, "none"));
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
      <PreviewDialog
        feed={previewing}
        open={previewing !== null}
        onClose={() => setPreviewing(null)}
        onAdd={addPreviewed}
        adding={add.isPending}
      />
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
              onPreview={setPreviewing}
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

/**
 * A URL as somebody would say it out loud.
 *
 * The scheme, a leading www and a bare trailing slash all carry nothing — every site here is
 * served over one of two schemes and the difference is not this list's business — and dropping
 * them is most of what makes an address short enough to sit under a name without eliding.
 *
 * Left alone if it will not parse. A feed whose site URL is malformed is a thing to show as
 * it is rather than a thing to guess at, and the browser will say so when it is clicked.
 */
function plainly(raw: string) {
  try {
    const url = new URL(raw);
    const host = url.host.startsWith("www.") ? url.host.slice(4) : url.host;
    const shown = host + url.pathname + url.search;
    return shown.endsWith("/") ? shown.slice(0, -1) : shown;
  } catch {
    return raw;
  }
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
  const [previewing, setPreviewing] = useState(false);

  const failing = feed.failure_count > 0;
  // Full paths, so a nested tag reads as "News / World" rather than losing where it sits.
  const labels = feed.tag_ids.map((id) => tagLabel(tags, id)).filter(Boolean);

  return (
    <div className="border-b border-rule py-4">
      {/* Side by side on a wide screen, stacked on a narrow one.

          The priority control is a fixed ~16rem — a label that must not resize plus a
          track — which on a phone leaves nothing for the name, and `truncate` duly
          truncated it to nothing.

          So on a narrow screen it is four lines: the name, where it is filed, whatever was
          written about it, and then the slider. `order` rather than a second copy of the markup, because the slider
          belongs beside the name on a wide screen and under everything on a narrow one. */}
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1.5">
        <div className="order-1 flex min-w-0 basis-full flex-col sm:flex-1 sm:basis-auto">
          {/* The name is the way into everything else about this feed. One affordance
              rather than a pencil for the title and a disclosure for the rest. */}
          <button
            type="button"
            onClick={() => onOpen(feed)}
            title={feed.title}
            className="truncate text-left font-serif text-lg leading-tight text-ink
              hover:text-accent"
          >
            {feed.title}
          </button>

          {/* Where the feed comes from, as a way back to it.
              
              A name is often not enough to place a feed a year later — "Notes", "Blog", a
              person's name — and the site itself answers in one click what no amount of
              metadata here would. It leaves this app, so it opens in its own tab.
              
              Dimmed and elided, because it is the least of what this row says and some feed
              addresses are a paragraph long. The scheme is dropped: nobody reads a feed list
              to find out that a site is served over https. */}
          {feed.site_url ? (
            <a
              href={feed.site_url}
              target="_blank"
              rel="noopener noreferrer"
              title={feed.site_url}
              className="mt-0.5 min-w-0 truncate text-xs text-ink-faint
                hover:text-ink-muted hover:underline hover:decoration-dotted
                hover:underline-offset-2"
            >
              {plainly(feed.site_url)}
            </a>
          ) : null}
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

        {/* Why this feed is here, when somebody has said.
            
            Last and full width, because it is prose rather than a field: it is the thing you
            read when the name has stopped being enough, and it would crowd the name if it sat
            beside it. Absent entirely when nothing was written, so a list of forty feeds with
            two notes in it shows two notes rather than thirty-eight blanks. */}
        {feed.note ? (
          <p
            className="order-3 basis-full font-serif text-sm leading-snug text-ink-muted
            sm:order-4"
          >
            {feed.note}
          </p>
        ) : null}

        {/* The one setting that stays in the list: it is a dial somebody nudges while
            looking at the whole of it, not something they go and open a feed to change. */}
        <div className="order-4 shrink-0 sm:order-2 sm:ml-auto">
          {/* What this feed is publishing today, without following it anywhere.

              The same dialog the picker uses before subscribing, which is the point: the
              question "is this still worth having" is the question "was this worth taking",
              asked later, and it deserves the same answer rather than a different screen.

              With the weight rather than under the name. Both are things done *to* a feed
              while looking at the list of them, and a control that sat beside the title read
              as part of the title — a word growing out of the name rather than a thing to
              press. Together they are the row's controls, in one place, at one weight.

              Inside the weight's own label rather than beside it, because beside it means at
              the far edge of a fixed-width box that is right-aligned against the track, and
              an icon alone in seventy pixels of white does not read as a button at all.

              An eye rather than the word, because next to a label that already reads "50 ·
              as usual" a second run of small text is one more thing to parse before finding
              the one to press. The name is on the button for anything not reading the
              picture, and the title is in it because a list of forty otherwise offers forty
              buttons that all say the same thing. */}
          <Priority
            label={`How often ${feed.title} appears`}
            value={feed.priority}
            onChange={(priority) =>
              update.mutate({ id: feed.id, changes: { priority } })
            }
            leading={
              <button
                type="button"
                onClick={() => setPreviewing(true)}
                aria-label={`Preview ${feed.title}`}
                title={`Preview ${feed.title}`}
                className="shrink-0 text-ink-faint hover:text-ink"
              >
                <EyeIcon className="text-base" />
              </button>
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

      {/* No `onAdd`: this feed is already followed, so there is nothing here to say yes to
          and the dialog closes rather than offering to do something. */}
      {previewing ? (
        <PreviewDialog
          feed={{ title: feed.title, feed_url: feed.url }}
          open={previewing}
          onClose={() => setPreviewing(false)}
        />
      ) : null}
    </div>
  );
}
