import { useState } from "react";

/**
 * A value shown once, with a button to take it away with you.
 *
 * The clipboard write can fail — an insecure origin, a browser that refuses without a
 * gesture it recognises — so the value stays selectable text either way. A copy button
 * that silently does nothing is worse than no button.
 */
export function CopyBox({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="flex items-center gap-2 rounded-md border border-rule bg-paper-sunken p-2">
      <code className="flex-1 overflow-x-auto text-xs break-all text-ink select-all">
        {value}
      </code>
      <button
        type="button"
        onClick={() => void copy()}
        className="shrink-0 rounded border border-rule bg-paper-raised px-2 py-1 text-xs text-ink-muted hover:text-ink"
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}
