import type { ReactNode } from "react";

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
      <p className="font-serif text-3xl leading-none tracking-tight text-ink">
        bystander
      </p>
      <h1 className="mt-8 font-serif text-2xl text-ink">{title}</h1>
      {intro ? (
        <div className="mt-2 text-sm text-ink-muted">{intro}</div>
      ) : null}
      <div className="mt-6 flex flex-col gap-4">{children}</div>
    </main>
  );
}
