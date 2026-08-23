import type { ReactNode } from "react";
import { NavLink } from "react-router";

import type { Me } from "@app/api/types";
import { Masthead } from "@app/components/Masthead";

const tabs = [
  { to: "/manage", label: "Feeds", end: true },
  { to: "/manage/tags", label: "Tags", end: false },
  { to: "/manage/settings", label: "Your page", end: false },
];

export function Layout({ me, children }: { me: Me; children: ReactNode }) {
  return (
    <>
      <Masthead me={me} subtitle={`Signed in as ${me.username}`} />
      <main className="mx-auto max-w-3xl px-6 py-10">
        <nav className="mb-8 flex gap-1 border-b border-rule">
          {tabs.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                `-mb-px border-b-2 px-3 py-2 text-sm ${
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
