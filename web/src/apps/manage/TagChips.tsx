import type { Tag } from "@app/api/types";
import { tagLabel } from "@app/lib/tags";

/**
 * Your own tags, as a row of things to switch on and off.
 *
 * One component because filing is one gesture wherever it happens: on a feed you already
 * follow, and on one you are deciding whether to. Two copies of this drifted once already —
 * the dialog grew an empty state the preview did not have — and the chips are the part
 * somebody recognises from screen to screen.
 *
 * Only tags that exist. The picker for an imported list has a second kind of chip, dashed,
 * for tags a source named that nobody here has yet — that belongs there, where a list arrived
 * carrying a taxonomy somebody has to accept or refuse. Here there is no list and no source:
 * there are your tags, and a way to make another.
 */
export function TagChips({
  tags,
  chosen,
  onToggle,
  onNew,
}: {
  tags: Tag[];
  /** Tag ids currently on. */
  chosen: string[];
  onToggle: (id: string) => void;
  /**
   * Offer a way to make one, and do this when it is pressed.
   *
   * Optional, because not every place that files a feed has room to open a second dialog.
   * Where it is given it matters most in the empty state: filing is the moment somebody
   * knows where a thing belongs, and "no tags yet" without a way to make one is a dead end
   * reached at exactly the wrong moment.
   */
  onNew?: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {tags.length === 0 ? (
        <p className="text-xs text-ink-faint">
          No tags yet. Tags are how you say which kinds of thing appear more
          often.
        </p>
      ) : (
        tags.map((tag) => {
          const on = chosen.includes(tag.id);
          return (
            <button
              key={tag.id}
              type="button"
              aria-pressed={on}
              onClick={() => onToggle(tag.id)}
              className={`rounded-full border px-2.5 py-0.5 text-xs ${
                on
                  ? "border-accent bg-accent/10 text-accent"
                  : "border-rule text-ink-muted hover:text-ink"
              }`}
            >
              {tagLabel(tags, tag.id)}
            </button>
          );
        })
      )}

      {/* Dashed, like the picker's chips for tags that do not exist yet, because it means
          the same thing here: press this and there will be one more tag than there was. */}
      {onNew ? (
        <button
          type="button"
          onClick={onNew}
          className="rounded-full border border-dashed border-ink-faint px-2.5 py-0.5 text-xs
            text-ink-muted hover:border-ink hover:text-ink"
        >
          New tag +
        </button>
      ) : null}
    </div>
  );
}
