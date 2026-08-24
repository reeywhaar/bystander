import { useEffect, useMemo, useState } from "react";

import type { Page, Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import {
  StanceSwitch,
  stanceOf,
  withStance,
  type Stance,
} from "@app/components/ui/StanceSwitch";
import {
  useCreatePage,
  useFeeds,
  useTags,
  useUpdatePage,
} from "@app/queries/hooks";

/**
 * What each position of a tag's switch means.
 *
 * The tags are a funnel: anything on the take side narrows the page to what carries it, and
 * anything on the drop side is removed afterwards. The order matters and is the reason there
 * are two sides rather than one — "Finance, but not the feeds that are also Crypto" needs the
 * narrowing to have happened before the removing.
 */
const TAG_SAYS: Record<Stance, string> = {
  exclude: "drop anything with this tag",
  neutral: "no opinion",
  include: "draw from this tag",
};

/**
 * And what they mean for a feed, which is not the same thing.
 *
 * A feed's switch overrides whatever the tags decided, in both directions. That is a stronger
 * thing than a second funnel and a more useful one: the two gestures anybody actually makes
 * about one publisher are "this one as well" and "this one never", and a filter that could only
 * narrow could express neither.
 */
const FEED_SAYS: Record<Stance, string> = {
  exclude: "never on this page",
  neutral: "whatever the tags decide",
  include: "always on this page",
};

/** The two sides of one list, held together because a change needs both to be applied. */
type Sides = { include: string[]; exclude: string[] };

const NOTHING: Sides = { include: [], exclude: [] };

/** Whether a subscription gets through the tag funnel, which is what "no opinion" defers to. */
function passesTags(
  sub: Subscription,
  include: string[],
  exclude: string[],
): boolean {
  if (include.length > 0 && !sub.tag_ids.some((id) => include.includes(id)))
    return false;
  return !sub.tag_ids.some((id) => exclude.includes(id));
}

/** A slug proposed from a name, so nobody has to think about URLs to make a page. */
function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

/**
 * One list of names, each with a switch, and a note beside the ones that are deferring.
 *
 * The note is only on the neutral rows and only where there is something to defer to. A feed
 * left alone follows the tags, and which way that lands it is not visible from the switch — so
 * a page could be saved with a feed somebody believed was on it and is not. Saying "in" or
 * "out" beside the middle position is the difference between a control that shows its state and
 * one that shows half of it.
 */
function StanceList({
  items,
  include,
  exclude,
  onChange,
  says,
  empty,
}: {
  items: { id: string; label: string; defersTo?: boolean }[];
  include: string[];
  exclude: string[];
  onChange: (id: string, stance: Stance) => void;
  says: Record<Stance, string>;
  empty: string;
}) {
  if (items.length === 0) {
    return <p className="text-sm text-ink-muted">{empty}</p>;
  }
  return (
    <div className="max-h-52 overflow-y-auto rounded-md border border-rule">
      {items.map((item) => {
        const stance = stanceOf(item.id, include, exclude);
        return (
          <div
            key={item.id}
            className="flex items-center gap-3 px-2 py-1.5 text-sm"
          >
            <span
              className={`flex-1 truncate ${
                stance === "exclude" ? "text-ink-faint" : "text-ink"
              }`}
            >
              {item.label}
            </span>
            {stance === "neutral" && item.defersTo !== undefined ? (
              // Tinted rather than coloured. It answers the same question the switch either
              // side of it answers, so it should read in the same terms — but it is a note
              // about what the tags did, not a control, and at full strength a column of them
              // shouts over the switches they are annotating.
              <span
                className={`text-xs ${
                  item.defersTo ? "text-positive-quiet" : "text-negative-quiet"
                }`}
              >
                {item.defersTo ? "in" : "out"}
              </span>
            ) : null}
            <StanceSwitch
              value={stance}
              onChange={(next) => onChange(item.id, next)}
              name={item.label}
              says={says}
            />
          </div>
        );
      })}
    </div>
  );
}

/**
 * Makes a page, or edits one.
 *
 * Everything is saved in one gesture, because half a filter is a page drawing from the wrong
 * things — a mode changed without its list is not a state anybody meant to be in, however
 * briefly. So the dialog holds a draft and sends it whole.
 *
 * The cadence and size are deliberately not here. They belong beside the page rather than
 * behind a save button: turning a page from daily to hourly is one decision with one obvious
 * effect, and burying it in a modal with a filter is how a simple control becomes a form.
 */
export function PageDialog({
  page,
  open,
  onClose,
}: {
  /** The page being edited, or null to make a new one. */
  page: Page | null;
  open: boolean;
  onClose: (saved?: Page) => void;
}) {
  const tags = useTags();
  const feeds = useFeeds();
  const create = useCreatePage();
  const update = useUpdatePage();

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  // Whether the address has been typed into. Until it has, it follows the name — which is what
  // somebody naming a page expects, and it stops being a field anybody has to think about.
  const [slugTouched, setSlugTouched] = useState(false);
  // Both sides of each list in one piece of state, so a change can be applied to what is
  // actually there rather than to what the last render closed over. Two separate pieces would
  // read the same stale pair twice if two switches ever moved in one tick, and the second
  // answer would quietly undo the first.
  const [tagSides, setTagSides] = useState<Sides>(NOTHING);
  const [feedSides, setFeedSides] = useState<Sides>(NOTHING);

  // Reset whenever the dialog opens on something else. A draft is a snapshot of the page it
  // was opened on, and leaving the last one behind would show somebody else's filter.
  useEffect(() => {
    if (!open) return;
    setName(page?.name ?? "");
    setSlug(page?.slug ?? "");
    setSlugTouched(Boolean(page));
    setTagSides({
      include: page?.include_tag_ids ?? [],
      exclude: page?.exclude_tag_ids ?? [],
    });
    setFeedSides({
      include: page?.include_feed_ids ?? [],
      exclude: page?.exclude_feed_ids ?? [],
    });
  }, [open, page]);

  // Every feed, not the ones the tags left. A feed's switch overrides the tags, so the feed
  // the tags dropped is exactly the one somebody might want to put back — offering only what
  // already gets through would hide the rows the control exists for.
  const feedRows = useMemo(
    () =>
      (feeds.data ?? []).map((sub: Subscription) => ({
        id: sub.feed_id,
        label: sub.title,
        defersTo: passesTags(sub, tagSides.include, tagSides.exclude),
      })),
    [feeds.data, tagSides],
  );

  const setTag = (id: string, stance: Stance) =>
    setTagSides((cur) => withStance(id, stance, cur.include, cur.exclude));
  const setFeed = (id: string, stance: Stance) =>
    setFeedSides((cur) => withStance(id, stance, cur.include, cur.exclude));

  const saving = create.isPending || update.isPending;
  const error = create.error ?? update.error;

  const submit = async () => {
    // All four sides, always. One request describes the whole filter, so there is no order in
    // which a reader could catch it half-applied — which matters here because the sides mean
    // different things together than they do apart.
    const body = {
      include_tag_ids: tagSides.include,
      exclude_tag_ids: tagSides.exclude,
      include_feed_ids: feedSides.include,
      exclude_feed_ids: feedSides.exclude,
    };

    if (page) {
      const changes = page.is_main
        ? body
        : { ...body, name: name.trim(), slug };
      const saved = await update.mutateAsync({ id: page.id, changes });
      onClose(saved);
      return;
    }
    const made = await create.mutateAsync({ name: name.trim(), slug });
    // A new page is filtered in a second request rather than at creation, because creating
    // takes a name and an address and nothing else — and a page that failed to be filtered
    // should still exist rather than silently not have been made.
    const saved = await update.mutateAsync({ id: made.id, changes: body });
    onClose(saved);
  };

  const named = page?.is_main || (name.trim() !== "" && slug !== "");

  return (
    <Modal
      open={open}
      onClose={() => onClose()}
      title={page ? `Edit ${page.name}` : "A new page"}
      footer={
        <>
          <Button onClick={() => onClose()} disabled={saving}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={() => void submit()}
            disabled={saving || !named}
          >
            {saving ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-6">
        {/* The main page's name and address are fixed, so there are no inputs for them —
            rather than inputs that refuse. */}
        {page?.is_main ? (
          <p className="text-sm text-ink-muted">
            Your main page is always here, at the root, and keeps its name. What
            it draws from is yours to choose.
          </p>
        ) : (
          <>
            <Field
              label="Name"
              value={name}
              onChange={(event) => {
                setName(event.target.value);
                if (!slugTouched) setSlug(slugify(event.target.value));
              }}
              placeholder="Finances"
              maxLength={60}
              autoFocus
            />
            <Field
              label="Address"
              hint={`It will live at /f/${slug || "…"}`}
              value={slug}
              onChange={(event) => {
                setSlugTouched(true);
                setSlug(event.target.value);
              }}
              placeholder="finances"
              maxLength={40}
            />
          </>
        )}

        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium text-ink">Tags</span>
          <p className="text-xs text-ink-faint">
            Leave a tag alone and it says nothing. Push one right and this page
            draws only from tags pushed right; push one left and it drops what
            carries that tag afterwards — which is how a finance page loses the
            crypto half of itself.
          </p>
          <StanceList
            items={(tags.data ?? []).map((tag: Tag) => ({
              id: tag.id,
              label: tag.name,
            }))}
            include={tagSides.include}
            exclude={tagSides.exclude}
            onChange={setTag}
            says={TAG_SAYS}
            empty="You have no tags yet. Tag a few feeds and they will show up here."
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium text-ink">Feeds</span>
          <p className="text-xs text-ink-faint">
            A feed overrules the tags. Right is always on this page, left is
            never, and left alone it follows the tags — the word beside it says
            where that lands it.
          </p>
          <StanceList
            items={feedRows}
            include={feedSides.include}
            exclude={feedSides.exclude}
            onChange={setFeed}
            says={FEED_SAYS}
            empty="You follow no feeds yet."
          />
        </div>

        {error ? <Alert>{error.message}</Alert> : null}
      </div>
    </Modal>
  );
}
