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
 * It re-measures on resize and when the headline faces finish loading, because both change how
 * tall a card is. Images do not need it: every one of them carries an explicit aspect ratio, so
 * its height is known before a byte of it arrives.
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

    // A card's height depends on its width, so every resize is a new set of measurements.
    //
    // On width alone, though. Packing changes the grid's *height*, which would call this
    // observer straight back — it settles after a round or two, since the second pass
    // measures the same heights and writes the same spans, but a loop that relies on
    // converging is a loop, and the browser says so in the console. A card's height does not
    // depend on the grid's, so there is nothing to recompute when only that changed.
    //
    // Guarded, because jsdom has no ResizeObserver and a test rendering this page should see
    // the page rather than a crash. Packing has already run by this point, so what a test
    // loses is only the response to a resize — and nothing resizes in jsdom.
    let width = node.getBoundingClientRect().width;
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => {
            const now = node.getBoundingClientRect().width;
            if (now === width) return;
            width = now;
            pack();
          });
    observer?.observe(node);

    // The headline faces are downloaded, and a headline set in the fallback is a different
    // number of lines from the same headline set in Oswald.
    let live = true;
    void document.fonts?.ready.then(() => {
      if (live) pack();
    });

    return () => {
      live = false;
      observer?.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
