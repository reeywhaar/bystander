import { NavLink } from "react-router";

import type { Page } from "@app/api/types";
import { useScrollingStrip } from "@app/components/ui/TabStrip";
import { usePages } from "@app/queries/hooks";

/** Where a page is read. The main one is at the root; the rest are addressed by their slug. */
export function addressOf(page: Page): string {
  return page.is_main ? "/" : `/f/${page.slug}`;
}

/**
 * The strip of front pages, under the masthead.
 *
 * Client-side links rather than anchors, which is a departure from how this application moves
 * between islands — those are separate documents on purpose. Here the pages are one island and
 * one document, and the reason is the cache: every page's edition is held under its own key, so
 * switching tabs shows a page that is already in hand rather than fetching it again. A full
 * navigation would throw all of that away, and with it the seeded layout, which would be
 * re-drawn identically but not instantly.
 *
 * Nothing is shown at all until there are two. Somebody who has never made a second page should
 * not have to look at a control for choosing between one thing.
 *
 * It scrolls sideways rather than wrapping, as the strip in the other islands does and for the
 * same reasons — see TabStrip. It wrapped, and on a phone four pages and Read later made two rows.
 */
export function PageTabs() {
  const pages = usePages();
  const all = pages.data ?? [];
  const { strip, more } = useScrollingStrip<HTMLDivElement>(all.length);
  if (all.length < 2) return null;

  return (
    <nav aria-label="Your front pages" className="border-b border-rule">
      {/* The page's margins outside the scroller, so the fade lands at the edge of the text
          column rather than the edge of the screen. */}
      <div className="mx-auto max-w-[1400px] px-6">
        <div
          ref={strip}
          className={`tab-strip flex items-center gap-x-5 overflow-x-auto py-2 text-sm ${
            more ? "tab-strip-more" : ""
          }`}
        >
          {all.map((page) => (
            <NavLink
              key={page.id}
              to={addressOf(page)}
              // `end` on the main page only: without it "/" would count as active while the
              // reader is on /f/anything, and the strip would show two tabs lit at once.
              end={page.is_main}
              // `shrink-0` and `whitespace-nowrap` keep a name whole rather than folding it to
              // make room — see TabStrip.
              className={({ isActive }) =>
                `shrink-0 border-b-2 pb-1 whitespace-nowrap ${
                  isActive
                    ? "border-accent text-ink"
                    : "border-transparent text-ink-faint hover:text-ink"
                }`
              }
            >
              {page.name}
            </NavLink>
          ))}
        </div>
      </div>
    </nav>
  );
}
