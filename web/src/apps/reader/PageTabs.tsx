import { NavLink } from "react-router";

import type { Page } from "@app/api/types";
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
 */
export function PageTabs() {
  const pages = usePages();
  const all = pages.data ?? [];
  if (all.length < 2) return null;

  return (
    <nav aria-label="Your pages" className="border-b border-rule">
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-center gap-x-5 gap-y-1 px-6 py-2 text-sm">
        {all.map((page) => (
          <NavLink
            key={page.id}
            to={addressOf(page)}
            // `end` on the main page only: without it "/" would count as active while the
            // reader is on /f/anything, and the strip would show two tabs lit at once.
            end={page.is_main}
            className={({ isActive }) =>
              isActive
                ? "border-b-2 border-accent pb-1 text-ink"
                : "border-b-2 border-transparent pb-1 text-ink-faint hover:text-ink"
            }
          >
            {page.name}
          </NavLink>
        ))}
      </div>
    </nav>
  );
}
