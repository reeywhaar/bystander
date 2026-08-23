import type { ImportResult } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";

/**
 * What an import did, said plainly.
 *
 * The summary only. What to do next is a dialog's action row, and a component that carried
 * its own buttons into one would be a second opinion about where they go.
 */
export function ImportOutcome({ result }: { result: ImportResult }) {
  return (
    <>
      <p className="text-sm text-ink">
        Added {result.added} feed{result.added === 1 ? "" : "s"}
        {result.skipped > 0
          ? ", skipped " + result.skipped + " you already follow"
          : ""}
        .
      </p>
      {result.tags_created.length > 0 ? (
        <p className="text-xs text-ink-muted">
          New tags: {result.tags_created.join(", ")}
        </p>
      ) : null}
      {result.failed.length > 0 ? (
        <Alert>
          {result.failed.length} could not be added:{" "}
          {result.failed.map((failure) => failure.feed_url).join(", ")}
        </Alert>
      ) : null}
    </>
  );
}
