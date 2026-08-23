import type { ReactNode } from "react";

/**
 * A refusal, or a note about one.
 *
 * The server writes its sentences for the person who will read them, so they are shown as
 * they arrive rather than being rewritten here.
 */
export function Alert({
  tone = "error",
  children,
}: {
  tone?: "error" | "note";
  children: ReactNode;
}) {
  const styles =
    tone === "error"
      ? "border-accent/40 bg-accent/8 text-accent"
      : "border-rule bg-paper-sunken text-ink-muted";
  return (
    <div
      role={tone === "error" ? "alert" : undefined}
      className={`rounded-md border px-3 py-2 text-sm ${styles}`}
    >
      {children}
    </div>
  );
}
