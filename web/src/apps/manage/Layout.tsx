import type { ReactNode } from "react";

import type { Me } from "@app/api/types";
import { Masthead } from "@app/components/Masthead";
import { TabStrip, type Tab } from "@app/components/ui/TabStrip";

const tabs: Tab[] = [
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
        <TabStrip tabs={tabs} />
        {children}
      </main>
    </>
  );
}
