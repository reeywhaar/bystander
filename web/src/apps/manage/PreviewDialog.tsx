import { useEffect, useState } from "react";

import type { PreviewItem, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { Spinner } from "@app/components/ui/Spinner";
import { since } from "@app/lib/time";
import { usePreviewFeed } from "@app/queries/hooks";

import { NewTagDialog } from "@app/apps/manage/NewTagDialog";
import { TagChips } from "@app/apps/manage/TagChips";

/**
 * What a feed has published, before anybody follows it.
 *
 * A feed's title and address say almost nothing about it. A site offering "Posts",
 * "Comments" and "Notes" is three plausible names and one right answer, and the only way to
 * find out used to be to follow one and look — which meant unfollowing it again, and the read
 * marks and the schedule that came with it. Ten articles answer the question in one screen.
 *
 * Wide, because this is the one dialog here that is read rather than answered. A form's width
 * would set a publisher's prose to forty characters a line, which is the shape every other
 * measure in this product exists to avoid.
 *
 * Nothing is stored by looking. The server fetches, parses and hands back ten items, so a feed
 * somebody opened and did not want leaves nothing behind at all.
 */
export function PreviewDialog({
  feed,
  open,
  onClose,
  onAdd,
  adding = false,
  filing,
}: {
  /** What to look at: a title to show while it loads, and the address to fetch. */
  feed: { title: string; feed_url: string } | null;
  open: boolean;
  onClose: () => void;
  /**
   * What the button at the bottom does, which is not the same in all three places it is used.
   *
   * Asked for one feed it subscribes; asked from a list of several it ticks that one and
   * hands the choice back. Either way the person pressing it has just read the thing and is
   * saying yes to it, which is why it is one button and not two dialogs.
   *
   * Left out when the feed is already followed, and then there is nothing to say yes to: the
   * dialog is being read rather than answered, so it closes and that is all. An Add there
   * would be a button that either does nothing or unfollows, and neither is what it says.
   */
  onAdd?: () => void;
  adding?: boolean;

  /**
   * Somewhere to file it, for the one case where pressing Add actually subscribes.
   *
   * Left out over the picker, where each row already carries its own chips and a second set
   * here would be two answers to one question. Left out for a feed already followed, where
   * the filing belongs to that feed's own dialog.
   *
   * Here because this is the moment somebody knows where a feed goes: they have just read
   * ten of its articles. Adding it untagged meant finding it again in the list afterwards to
   * say the thing they already knew.
   */
  filing?: {
    tags: Tag[];
    /** Tag ids currently on. */
    chosen: string[];
    onToggle: (id: string) => void;
    onCreated: (tag: Tag) => void;
  };
}) {
  const preview = usePreviewFeed();
  const [makingTag, setMakingTag] = useState(false);

  // Asked for when the dialog opens on something, and asked again when it opens on something
  // else. A mutation rather than a query, so nothing here is cached — pressing Preview a
  // second time after leaving the dialog open asks the publisher again, which is what the
  // word means.
  const { mutate } = preview;
  const url = feed?.feed_url;
  useEffect(() => {
    if (open && url) mutate(url);
  }, [open, url, mutate]);

  return (
    <Modal
      wide
      flush
      onPaper
      open={open}
      onClose={onClose}
      title={feed?.title || "This feed"}
      footer={
        onAdd ? (
          <>
            <Button onClick={onClose} disabled={adding}>
              Cancel
            </Button>
            <Button variant="primary" onClick={onAdd} disabled={adding}>
              {adding ? "Adding…" : "Add"}
            </Button>
          </>
        ) : (
          <Button variant="primary" onClick={onClose}>
            Close
          </Button>
        )
      }
    >
      {/* Above the articles, and outside the box they scroll in. It is part of the same
          decision as the Add below it, so it must not be something you scroll away from —
          and this dialog is several screens tall by design. */}
      {filing ? (
        <div className="flex flex-col gap-1.5 border-b border-rule pb-3">
          <p className="text-xs text-ink-muted">File it under</p>
          <TagChips
            tags={filing.tags}
            chosen={filing.chosen}
            onToggle={filing.onToggle}
            onNew={() => setMakingTag(true)}
          />
        </div>
      ) : null}

      {preview.isPending ? (
        <Spinner />
      ) : preview.error ? (
        <Alert>{preview.error.message}</Alert>
      ) : preview.data ? (
        <Preview items={preview.data.items} />
      ) : null}

      {makingTag && filing ? (
        <NewTagDialog
          open
          tags={filing.tags}
          onClose={() => setMakingTag(false)}
          onCreated={filing.onCreated}
        />
      ) : null}
    </Modal>
  );
}

function Preview({ items }: { items: PreviewItem[] }) {
  if (items.length === 0) {
    return (
      <p className="text-sm text-ink-muted">
        This feed has nothing in it yet. It may be new, or it may have been
        emptied — either way there is nothing to judge it by.
      </p>
    );
  }

  return (
    /*
      The articles scroll inside the dialog rather than taking it with them.
      
      Ten of them with pictures is several screens, and the whole dialog scrolling meant
      reaching Add by scrolling past everything — the button that ends the job was the hardest
      thing in the dialog to reach. Bounded in viewport units because the content is tall and
      a fixed height would be wrong on both a phone and a desktop; 55 leaves room for the
      title above and the buttons below at any size worth designing for.
    */
    <div className="max-h-[55dvh] overflow-y-auto overscroll-contain pr-1">
      {items.map((item) => (
        /*
          The page's own article styling, not a plainer one invented here. This is a sample of
          what these would look like once followed, so it is set the way they would be: the
          slot decides the sizes, the voice decides the face.
          
          `slot-standard` rather than a bigger one: it is the size most cards on the page are,
          and a feature's twenty-four pixels of bold, set ten deep, outweighed the feed's own
          name at the top of the dialog — the subject reading as a caption on its samples. The
          slot also carries a grid span, which does nothing outside the grid; it is here for
          the sizes it brings with it, which is the whole reason the slot is one class rather
          than four separate ones.
        */
        <article
          key={item.link || item.title}
          // `-of-type` rather than `first`/`last`: the spacer below is the last child now, so
          // `last:border-b-0` stopped matching the last article and it drew a rule of its own
          // right above the footer's. Matching on the element type is what makes the rules
          // about articles rather than about whatever happens to be last in the box.
          className="slot-standard border-b border-rule py-4 first-of-type:pt-0 last-of-type:border-b-0 last-of-type:pb-0"
        >
          {/*
            One voice for all ten rather than the page's per-article draw. On the page the
            faces vary because the cards do — different widths, different sizes, spread across
            a grid; ten of them stacked in one column at one width would be a ransom note.
            The workhorse is the one that says nothing about itself, which is right here: the
            subject is the feed, not the typography.
          */}
          <h3 className="headline voice-workhorse text-ink">
            <a
              href={item.link}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:underline underline-offset-4"
            >
              {item.title}
            </a>
          </h3>
          <p className="mt-1 text-xs text-ink-faint">
            {since(item.published_at)}
          </p>
          {item.image_url ? (
            // Whole, not cropped. The page crops to a drawn shape because a page is a
            // composition; this is a sample, and a comic with its punchline cut off answers
            // the question wrongly.
            <img
              src={item.image_url}
              alt=""
              loading="lazy"
              // Bounded both ways. Height, because ten unbounded comics is a dialog nobody
              // can scan; width, because a picture wider than the dialog would run out of
              // it — `w-auto` alone does not stop that, it only stops the browser
              // stretching. Twenty rem is a comic still readable at a glance.
              className="mt-2 max-h-80 max-w-full rounded-sm border border-rule object-contain"
              // A publisher's image that 404s or hotlink-blocks would otherwise leave a
              // broken-image glyph in the middle of the preview.
              onError={(event) => {
                event.currentTarget.style.display = "none";
              }}
            />
          ) : null}
          {item.summary ? (
            // Sanitized on the server at parse, by the pass every stored summary goes
            // through. It is not sanitized again here on purpose: a second sanitizer is a
            // second thing to be wrong, and the safe form is what was sent. See
            // internal/feeds/sanitize.go.
            <div
              className="prose-summary prose-step-1 mt-2 text-ink-muted"
              dangerouslySetInnerHTML={{ __html: item.summary }}
            />
          ) : null}
        </article>
      ))}

      {/* The room after the last article — see above for why it is not padding. */}
      <div aria-hidden className="h-5" />
    </div>
  );
}
