import { useLayoutEffect, type RefObject } from "react";

/**
 * The height of one grid row, in pixels.
 *
 * The grid is laid out on rows this tall and every card spans as many of them as it needs, so
 * this is the granularity with which a card can end. Eight is small enough that the rounding
 * is invisible and large enough that a page of fifty cards is not a grid of six thousand rows.
 */
const ROW = 8;

/**
 * Packs the front page vertically, the way masonry does.
 *
 * A grid row is as tall as the tallest card in it, so a short story beside a long one leaves
 * the difference empty — measured at nearly a third of the page. Masonry is the answer to
 * exactly that, and there is no CSS for it yet: `grid-template-rows: masonry`, `display:
 * masonry` and `item-pack` are all unsupported in current Chromium, which was checked rather
 * than assumed.
 *
 * So the packing is done inside CSS Grid instead of outside it. Every row is eight pixels and
 * each card is told how many of them it spans, which lets `grid-auto-flow: dense` fill the
 * space under a short card with a later one. That is the whole trick, and it is why this is
 * not a masonry library: **nothing is positioned here.** The grid still decides where things
 * go, so the column widths, the dense backfill and the full-width rules all keep working —
 * a library would have had to reimplement each of them.
 *
 * The cost is worth naming precisely, because it is easy to name wrongly. This is **not**
 * non-deterministic: the same page at the same width with the same faces packs the same way
 * every time, whatever measures it, because it is a function of heights that are themselves a
 * function of the content. Reloading does not reshuffle anything, and "where an article sits
 * is how somebody remembers where they were" survives intact.
 *
 * What it is not is *computable before paint*. The browser has to lay the page out once for
 * anything to have a height, so the page settles a frame later. On that first settle nothing
 * moves sideways — a card only rises to close a gap under the one above it — and the widest
 * cards are at the top, so what is on screen at the moment of loading is what moves least.
 *
 * Resizing the window does rearrange the page, and that is fine. A different width is a
 * different set of line breaks, so it was always going to be a different page; the reader is
 * the one doing it and is watching it happen.
 *
 * It re-measures whenever a card's height changes, rather than at the two moments a card's
 * height was expected to change. That was the bug: it re-packed on resize and on
 * `document.fonts.ready`, and anything that made a card taller afterwards left its span too
 * small — a card is not clipped, so it simply drew over the one below it. Safari was where this
 * showed, and blaming Safari would have been the wrong lesson: `fonts.ready` resolves when the
 * fonts *pending at that moment* have loaded, and this page picks one of six display faces per
 * article, so a face nothing had used yet could start loading after it resolved. Any browser
 * can do that. Watching the cards costs one observer and needs no list of the things that might
 * move them.
 *
 * Watching them is only safe because `.page-grid` sets `align-items: start`: a card hugs its
 * content, so writing its span cannot change its height, so packing cannot trigger the observer
 * that triggered the packing.
 */
export function useMasonry(
  grid: RefObject<HTMLElement | null>,
  // Re-run when the page's contents change. Not the items themselves: a card's height does
  // not depend on whether it has been read.
  deps: unknown[],
) {
  useLayoutEffect(() => {
    const node = grid.current;
    if (!node) return;

    function pack() {
      const el = grid.current;
      if (!el) return;

      for (const child of Array.from(el.children)) {
        if (!(child instanceof HTMLElement)) continue;

        // Cleared before measuring. A card still carrying last measurement's span would be
        // measured at that height rather than at the height its content wants, so the page
        // would only ever grow.
        child.style.gridRowEnd = "";
      }

      // Every read first, then every write. Interleaving them makes the browser lay the page
      // out again between each pair, which turns one reflow into one per card.
      const spans = Array.from(el.children).map((child) => {
        if (!(child instanceof HTMLElement)) return 1;

        // Margins included, and this is the whole of what makes the gaps exist.
        //
        // The grid's own `row-gap` has to be zero — it would otherwise apply between every
        // one of the eight-pixel rows a card spans — so the space between cards is a margin
        // on the cards themselves. A span measured from `getBoundingClientRect`, which stops
        // at the border box, leaves that margin outside the card's grid area, and the next
        // card packs straight up against it. Which is exactly what happened: a page of
        // stories touching each other, top to bottom.
        const box = getComputedStyle(child);
        const height =
          child.getBoundingClientRect().height +
          parseFloat(box.marginTop) +
          parseFloat(box.marginBottom);

        return Math.max(1, Math.ceil(height / ROW));
      });

      Array.from(el.children).forEach((child, i) => {
        if (child instanceof HTMLElement) {
          child.style.gridRowEnd = `span ${spans[i]}`;
        }
      });
    }

    pack();

    // Guarded, because jsdom has no ResizeObserver and a test rendering this page should see
    // the page rather than a crash. Packing has already run by this point, so what a test
    // loses is only the response to something moving — and nothing moves in jsdom.
    if (typeof ResizeObserver === "undefined") return;

    // The grid, for its width: a card's height depends on how wide it is, so a new width is a
    // new set of measurements. Its *height* is ignored, because packing is what changes that
    // and answering it would be answering ourselves.
    let width = node.getBoundingClientRect().width;

    // And every card, for its height. This is what catches a face arriving late, a picture
    // whose real shape was not known at first paint, or anything else that makes a card taller
    // after it was measured. Compared against the last height rather than trusted, because the
    // observer fires once on observe and would otherwise pack a second time for nothing.
    const heights = new WeakMap<Element, number>();

    const observer = new ResizeObserver((entries) => {
      let stale = false;
      for (const entry of entries) {
        if (entry.target === node) {
          const now = entry.contentRect.width;
          if (now !== width) {
            width = now;
            stale = true;
          }
          continue;
        }
        const now = entry.contentRect.height;
        if (heights.get(entry.target) !== now) {
          heights.set(entry.target, now);
          stale = true;
        }
      }
      if (stale) pack();
    });

    observer.observe(node);
    for (const child of Array.from(node.children)) {
      observer.observe(child);
    }

    return () => observer.disconnect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
