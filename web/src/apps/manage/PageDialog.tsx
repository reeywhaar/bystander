import { useEffect, useMemo, useState } from "react";

import type {
  FeedFilter,
  Page,
  Subscription,
  Tag,
  TagFilter,
} from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import {
  useCreatePage,
  useFeeds,
  useTags,
  useUpdatePage,
} from "@app/queries/hooks";

/**
 * What the tag control offers, and what each choice means in a sentence.
 *
 * A segmented control rather than a checkbox and a list, because there are three states and
 * two of them are opposites. "Not filtering" and "including nothing" would be the same empty
 * list under a checkbox, and they are not the same intention.
 */
const TAG_MODES: { value: TagFilter; label: string }[] = [
  { value: "no", label: "Any tag" },
  { value: "including", label: "Only these tags" },
  { value: "excluding", label: "All but these tags" },
];

const FEED_MODES: { value: FeedFilter; label: string }[] = [
  { value: "all", label: "Any feed" },
  { value: "including", label: "Only these feeds" },
  { value: "excluding", label: "All but these feeds" },
];

/**
 * Which feed modes make sense beside a given tag mode.
 *
 * Narrowing twice in the same direction is a control that cannot do anything: a page already
 * held to a set of tags does not also need "only these feeds", because the feeds it could pick
 * are the ones the tags already chose — and the useful second gesture is to drop one of them.
 * The same the other way round. So the second control offers the direction the first did not.
 */
function feedModesFor(tags: TagFilter): FeedFilter[] {
  switch (tags) {
    case "including":
      return ["all", "excluding"];
    case "excluding":
      return ["all", "including"];
    default:
      return ["all", "including", "excluding"];
  }
}

/** A slug proposed from a name, so nobody has to think about URLs to make a page. */
function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm font-medium text-ink">{label}</span>
      <div className="flex flex-wrap gap-2">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
            aria-pressed={option.value === value}
            className={`rounded-md border px-3 py-1.5 text-sm ${
              option.value === value
                ? "border-accent bg-accent/10 text-accent"
                : "border-rule text-ink-muted hover:text-ink"
            }`}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

/** A list of things to tick, which is what both filter lists are. */
function Picker({
  items,
  chosen,
  onToggle,
  empty,
}: {
  items: { id: string; label: string }[];
  chosen: string[];
  onToggle: (id: string) => void;
  empty: string;
}) {
  if (items.length === 0) {
    return <p className="text-sm text-ink-muted">{empty}</p>;
  }
  return (
    <div className="max-h-48 overflow-y-auto rounded-md border border-rule p-2">
      {items.map((item) => (
        <label
          key={item.id}
          className="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm text-ink hover:bg-ink/5"
        >
          <input
            type="checkbox"
            checked={chosen.includes(item.id)}
            onChange={() => onToggle(item.id)}
          />
          {item.label}
        </label>
      ))}
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
  const [tagMode, setTagMode] = useState<TagFilter>("no");
  const [feedMode, setFeedMode] = useState<FeedFilter>("all");
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [feedIDs, setFeedIDs] = useState<string[]>([]);

  // Reset whenever the dialog opens on something else. A draft is a snapshot of the page it
  // was opened on, and leaving the last one behind would show somebody else's filter.
  useEffect(() => {
    if (!open) return;
    setName(page?.name ?? "");
    setSlug(page?.slug ?? "");
    setSlugTouched(Boolean(page));
    setTagMode(page?.tag_filter ?? "no");
    setFeedMode(page?.feed_filter ?? "all");
    setTagIDs(page?.tag_ids ?? []);
    setFeedIDs(page?.feed_ids ?? []);
  }, [open, page]);

  const allowedFeedModes = feedModesFor(tagMode);

  // Which feeds the second control may offer, given what the first one already decided.
  //
  // Showing all of them would offer feeds that the tag filter has already excluded, where
  // ticking one does nothing — a control that accepts a gesture and produces no change is
  // worse than one that is not there.
  const feedChoices = useMemo(() => {
    const subs = feeds.data ?? [];
    const chosen = new Set(tagIDs);
    const matches = (sub: Subscription) =>
      sub.tag_ids.some((id) => chosen.has(id));

    const visible =
      tagMode === "including"
        ? subs.filter(matches)
        : tagMode === "excluding"
          ? subs.filter((sub) => !matches(sub))
          : subs;

    return visible.map((sub) => ({ id: sub.feed_id, label: sub.title }));
  }, [feeds.data, tagMode, tagIDs]);

  const toggle = (list: string[], id: string) =>
    list.includes(id) ? list.filter((x) => x !== id) : [...list, id];

  const saving = create.isPending || update.isPending;
  const error = create.error ?? update.error;

  const submit = async () => {
    // The lists are sent whatever the mode says, and the server clears the ones its mode does
    // not read. One request describes the whole page, so there is no order in which a reader
    // could catch it half-applied.
    const body = {
      tag_filter: tagMode,
      feed_filter: feedMode,
      tag_ids: tagMode === "no" ? [] : tagIDs,
      feed_ids: feedMode === "all" ? [] : feedIDs,
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

        <Segmented
          label="Tags"
          value={tagMode}
          options={TAG_MODES}
          onChange={(mode) => {
            setTagMode(mode);
            // The feed control means something different beside a different tag mode, and a
            // choice made against the old one is not a choice anybody made against this one.
            setFeedMode("all");
            setFeedIDs([]);
          }}
        />
        {tagMode !== "no" ? (
          <Picker
            items={(tags.data ?? []).map((tag: Tag) => ({
              id: tag.id,
              label: tag.name,
            }))}
            chosen={tagIDs}
            onToggle={(id) => setTagIDs((current) => toggle(current, id))}
            empty="You have no tags yet. Tag a few feeds and they will show up here."
          />
        ) : null}

        <Segmented
          label="Feeds"
          value={feedMode}
          options={FEED_MODES.filter((mode) =>
            allowedFeedModes.includes(mode.value),
          )}
          onChange={(mode) => {
            setFeedMode(mode);
            setFeedIDs([]);
          }}
        />
        {feedMode !== "all" ? (
          <Picker
            items={feedChoices}
            chosen={feedIDs}
            onToggle={(id) => setFeedIDs((current) => toggle(current, id))}
            empty="Nothing to choose from — the tags above have already narrowed this to nothing."
          />
        ) : null}

        {error ? <Alert>{error.message}</Alert> : null}
      </div>
    </Modal>
  );
}
