import { useEffect, useRef, useState } from "react";
import { NavLink, useLocation } from "react-router";

export interface Tab {
  to: string;
  label: string;
  /** Whether the path has to match exactly — for the tab that owns the section's root. */
  end?: boolean;
}

/**
 * The row of tabs under the masthead, in the islands that have sections.
 *
 * It scrolls sideways rather than wrapping. Wrapping was tried and is the safer-sounding of
 * the two — nothing can be out of sight — but five tabs on a phone became two rows, and a
 * two-row strip reads as two groups of things rather than one row of them.
 *
 * Scrolling has one real cost, and it is worth naming because it is the reason wrapping was
 * tried first: a tab off the end of the strip is a tab somebody may not know is there, and on
 * this nav that tab is Account, which is the way to sign out. Two things pay for it, and both
 * are below: the strip fades at the edge when there is more of it, so it looks cut off rather
 * than finished, and the tab you are on is scrolled into view, so the strip never opens
 * showing you somewhere you are not.
 */
export function TabStrip({ tabs }: { tabs: Tab[] }) {
  const strip = useRef<HTMLElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const { pathname } = useLocation();

  useEffect(() => {
    const node = strip.current;
    if (!node) return;

    const measure = () =>
      setOverflowing(node.scrollWidth > node.clientWidth + 1);
    measure();

    // The tab somebody is on, brought into the strip. `nearest` so a strip that already shows
    // it is left alone — scrolling a visible thing into view moves the page under a reader for
    // no reason.
    node
      .querySelector<HTMLElement>('[aria-current="page"]')
      ?.scrollIntoView({ inline: "nearest", block: "nearest" });

    // Guarded: jsdom has no ResizeObserver, and a test rendering a section should see the
    // section rather than a crash. What a test loses is the response to a resize, and nothing
    // resizes in jsdom.
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [pathname, tabs.length]);

  return (
    // The rule belongs to the wrapper rather than to the strip, so it runs the full width and
    // the fade below cannot eat its end. The strip scrolls inside it.
    <div className="mb-8 border-b border-rule pb-2">
      <nav
        ref={strip}
        className={`tab-strip flex gap-1 overflow-x-auto ${
          overflowing ? "tab-strip-more" : ""
        }`}
      >
        {tabs.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            end={tab.end}
            className={({ isActive }) =>
              // A tinted pill rather than an underline, and the same one the front pages use
              // for their own strip — two strips that do the same thing should not be two
              // different objects.
              //
              // `shrink-0` and `whitespace-nowrap` are both load-bearing on a strip that
              // scrolls: without them a tab shrinks and folds its own label — "Front / pages"
              // — rather than letting the strip run past the edge.
              `shrink-0 rounded-md px-3 py-1.5 text-sm whitespace-nowrap ${
                isActive
                  ? "bg-accent/10 text-accent"
                  : "text-ink-muted hover:text-ink"
              }`
            }
          >
            {tab.label}
          </NavLink>
        ))}
      </nav>
    </div>
  );
}
