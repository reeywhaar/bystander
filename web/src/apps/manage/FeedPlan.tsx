import type { ImportSelection, PlannedFeed, Tag } from "@app/api/types";
import { Button } from "@app/components/ui/Button";
import { Reach } from "@app/components/ui/Reach";
import { tagLabel, tagPath } from "@app/lib/tags";

/** A tag chip is identified by the tag it names, whether or not that tag exists yet. */
const ownKey = (id: string) => "id:" + id;
const newKey = (path: string[]) => "new:" + path.join("/");

/** What has been ticked: which feeds, and which tags on each. */
export interface PlanSelection {
  /** Feeds turned off. Absence means kept, so a new feed arrives ticked. */
  skipped: Set<string>;
  /** Feed URL to the tag chips chosen for it. */
  tags: Map<string, Set<string>>;
}

/**
 * Where a plan starts.
 *
 * On each feed, the tags the source named **that you already have**. Those are a match rather
 * than a decision. Tags you do not have start off: a taxonomy should arrive because somebody
 * asked for it, not because it came in the post.
 *
 * Whether the feeds themselves start ticked depends on where the list came from, and the two
 * cases genuinely differ. A pasted list is a list somebody chose — every feed in it is there
 * because they put it there, so the work is untangling the few they have changed their mind
 * about. A site that turns out to offer five feeds chose none of them: taking all five is
 * almost never what anybody wants, and a screen that starts by assuming it makes "None" the
 * first thing you have to press.
 */
export function initialSelection(
  feeds: PlannedFeed[],
  start: "all" | "none" = "all",
): PlanSelection {
  return {
    skipped:
      start === "none"
        ? new Set(feeds.map((feed) => feed.feed_url))
        : new Set(),
    tags: new Map(
      feeds.map((feed) => [
        feed.feed_url,
        new Set(
          feed.tags
            .filter((tag) => tag.tag_id)
            .map((tag) => ownKey(tag.tag_id)),
        ),
      ]),
    ),
  };
}

/**
 * Feeds already followed are never part of a plan.
 *
 * The server refuses a second subscription and reports it as skipped, so a row for one
 * would be a choice that does nothing — worse than no row, because it reads as filing
 * about to happen.
 */
export function offered(feeds: PlannedFeed[]): PlannedFeed[] {
  return feeds.filter((feed) => !feed.already_subscribed);
}

export function kept(
  feeds: PlannedFeed[],
  selection: PlanSelection,
): PlannedFeed[] {
  return offered(feeds).filter((feed) => !selection.skipped.has(feed.feed_url));
}

/** What to send: exactly what was ticked, and nothing about what was not. */
export function toImport(
  feeds: PlannedFeed[],
  selection: PlanSelection,
  tags: Tag[],
): ImportSelection[] {
  return kept(feeds, selection).map((feed) => {
    const keys = selection.tags.get(feed.feed_url) ?? new Set<string>();
    const paths: string[][] = [];

    for (const key of keys) {
      if (key.startsWith("id:")) {
        const path = tagPath(tags, key.slice(3));
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
      reach: feed.reach,
      tag_paths: paths,
    };
  });
}

/**
 * A list of feeds to be added, with somewhere to file each one.
 *
 * Shared by the two ways feeds arrive — a pasted list, and a site that offers several —
 * because after "where did these come from" the question is the same both times: which of
 * them do I want, and filed under what. Two screens would have drifted.
 */
export function FeedPlan({
  feeds,
  tags,
  selection,
  onChange,
  onPreview,
}: {
  feeds: PlannedFeed[];
  tags: Tag[];
  selection: PlanSelection;
  onChange: (selection: PlanSelection) => void;
  /**
   * Show what one of them has published, before it is taken.
   *
   * Optional, because the list can be shown somewhere with nowhere to put a dialog. Where it
   * is given, every row gets it: a title and an address are not enough to choose between
   * "Posts", "Comments" and "Notes", which is the list this screen most often shows.
   */
  onPreview?: (feed: PlannedFeed) => void;
}) {
  const showing = offered(feeds);
  const hidden = feeds.length - showing.length;
  const keeping = kept(feeds, selection);

  function setSkipped(next: Set<string>) {
    onChange({ ...selection, skipped: next });
  }

  function toggleTag(feedURL: string, key: string) {
    const tagsFor = new Map(selection.tags);
    const forFeed = new Set(tagsFor.get(feedURL) ?? []);
    if (forFeed.has(key)) forFeed.delete(key);
    else forFeed.add(key);
    tagsFor.set(feedURL, forFeed);
    onChange({ ...selection, tags: tagsFor });
  }

  if (showing.length === 0) {
    return (
      <p className="py-2 text-sm text-ink-muted">
        You already follow {feeds.length === 1 ? "that one" : "all of those"}.
      </p>
    );
  }

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          onClick={() => setSkipped(new Set())}
          disabled={keeping.length === showing.length}
        >
          All
        </Button>
        <Button
          onClick={() =>
            setSkipped(new Set(showing.map((feed) => feed.feed_url)))
          }
          disabled={keeping.length === 0}
        >
          None
        </Button>
        <span className="ml-auto text-xs text-ink-faint">
          {keeping.length} of {showing.length}
        </span>
      </div>

      {/* Counted even though they are not shown, so a list that overlaps heavily does not
          simply look shorter than the one that was sent. */}
      {hidden > 0 ? (
        <p className="text-xs text-ink-faint">
          {hidden} {hidden === 1 ? "is" : "are"} already yours, and not shown.
        </p>
      ) : null}

      <ul className="max-h-72 overflow-y-auto rounded-md border border-rule">
        {showing.map((feed) => (
          <PlanRow
            key={feed.feed_url}
            feed={feed}
            tags={tags}
            keep={!selection.skipped.has(feed.feed_url)}
            chosen={selection.tags.get(feed.feed_url) ?? new Set()}
            onKeep={(keep) => {
              const next = new Set(selection.skipped);
              if (keep) next.delete(feed.feed_url);
              else next.add(feed.feed_url);
              setSkipped(next);
            }}
            onToggleTag={(key) => toggleTag(feed.feed_url, key)}
            onPreview={onPreview ? () => onPreview(feed) : undefined}
          />
        ))}
      </ul>

      {tags.length > 0 || showing.some((feed) => feed.tags.length > 0) ? (
        <p className="text-xs text-ink-faint">
          Solid chips are your own tags. Dashed ones are new and would be
          created.
        </p>
      ) : null}
    </>
  );
}

/**
 * One feed, with every tag you have underneath it.
 *
 * All of them, not just the ones the source mentioned, because filing a feed is the moment
 * you actually know where it belongs — and the alternative is adding it and then going to
 * find it again.
 */
function PlanRow({
  feed,
  tags,
  keep,
  chosen,
  onKeep,
  onToggleTag,
  onPreview,
}: {
  feed: PlannedFeed;
  tags: Tag[];
  keep: boolean;
  chosen: Set<string>;
  onKeep: (keep: boolean) => void;
  onToggleTag: (key: string) => void;
  onPreview?: () => void;
}) {
  // Tags the source named that nobody here has yet.
  const incoming = feed.tags.filter((tag) => !tag.tag_id);

  return (
    <li className="border-b border-rule px-3 py-2.5 last:border-b-0">
      {/* The label covers the tick and the name and stops there. A button inside a label is
          a button that also ticks the box, which on this row would mean looking at a feed
          and thereby choosing it. */}
      <div className="flex items-baseline gap-2 text-sm">
        <label className="flex min-w-0 flex-1 cursor-pointer items-baseline gap-2">
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
            {/* What the list says this feed is worth reading back, which arrives with it. A
                setting somebody is about to accept should be visible before they accept it,
                not discovered afterwards in the feed's own dialog. */}
            <span className="mt-1 block">
              <Reach seconds={feed.reach} />
            </span>
          </span>
        </label>

        {onPreview ? (
          <button
            type="button"
            onClick={onPreview}
            className="shrink-0 rounded-md px-2 py-1 text-xs text-ink-muted hover:bg-paper-sunken hover:text-ink"
          >
            Preview
          </button>
        ) : null}
      </div>

      {keep && (tags.length > 0 || incoming.length > 0) ? (
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
