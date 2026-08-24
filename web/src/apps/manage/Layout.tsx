import type { ReactNode } from "react";
import { NavLink } from "react-router";

import type { Me } from "@app/api/types";
import { Masthead } from "@app/components/Masthead";

const tabs = [
  { to: "/manage", label: "Feeds", end: true },
  { to: "/manage/tags", label: "Tags", end: false },
  { to: "/manage/pages", label: "Front pages", end: false },
  { to: "/manage/read", label: "Recently read", end: false },
  { to: "/manage/account", label: "Account", end: false },
];

export function Layout({ me, children }: { me: Me; children: ReactNode }) {
  return (
    <>
      {/* No subtitle: the masthead now carries the name beside the person icon, and a band
          that said it twice would be a band saying it twice. */}
      <Masthead me={me} />
      <main className="mx-auto max-w-3xl px-6 py-10">
        {/* Wraps rather than scrolls.
         *
         * Five tabs do not fit across a phone: the row overflowed, "Account" was cut off at
         * the edge, and the labels that survived broke mid-name — "Front / pages". A
         * horizontally scrolling strip is the other convention and is wrong here, because the
         * tab it would hide is the one that leads to signing out.
         *
         * `whitespace-nowrap` is what stops the mid-name break: without it a tab shrinks by
         * folding its own label before the row ever wraps. */}
        <nav className="mb-8 flex flex-wrap gap-x-1 gap-y-0.5 border-b border-rule">
          {tabs.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                `-mb-px border-b-2 px-2 py-2 text-sm whitespace-nowrap sm:px-3 ${
                  isActive
                    ? "border-accent text-ink"
                    : "border-transparent text-ink-muted hover:text-ink"
                }`
              }
            >
              {tab.label}
            </NavLink>
          ))}
        </nav>
        {children}
      </main>
    </>
  );
}
