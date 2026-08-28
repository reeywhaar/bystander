import type { ReactNode } from "react";

import { Logo } from "@app/components/Logo";
import { Colophon } from "@app/components/Colophon";

/** The card every unauthenticated screen sits in. */
export function Frame({
  title,
  intro,
  children,
}: {
  title: string;
  intro?: ReactNode;
  children: ReactNode;
}) {
  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center px-6 py-16">
      {/* Not a link. This is the card somebody signs in from, and the one thing they came
          for is the form — a wordmark that navigates is a way out of it, offered first. */}
      <Logo className="h-[38px] w-auto text-ink" />
      <h1 className="mt-8 font-serif text-2xl text-ink">{title}</h1>
      {intro ? (
        <div className="mt-2 text-sm text-ink-muted">{intro}</div>
      ) : null}
      <div className="mt-6 flex flex-col gap-4">{children}</div>
      {/* Under the card rather than in it: it belongs to the page, not to the form. */}
      <Colophon className="mt-10" />
    </main>
  );
}
