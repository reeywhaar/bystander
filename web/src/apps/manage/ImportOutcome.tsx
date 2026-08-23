import type { ImportResult } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";

/** What an import did, said plainly. Shared by both ways feeds arrive. */
export function ImportOutcome({
  result,
  onAgain,
  onClose,
  againLabel = "Import another",
}: {
  result: ImportResult;
  onAgain?: () => void;
  onClose: () => void;
  againLabel?: string;
}) {
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
      <div className="flex justify-end gap-2">
        {onAgain ? <Button onClick={onAgain}>{againLabel}</Button> : null}
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </div>
    </>
  );
}
