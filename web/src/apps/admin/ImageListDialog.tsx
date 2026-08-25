import type { ImageFailure } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { Spinner } from "@app/components/ui/Spinner";
import { exact, until } from "@app/lib/time";
import { useRetryImages, useUnmeasuredImages } from "@app/queries/hooks";

/**
 * The pictures behind one of the counts on the images screen.
 *
 * The counts say what is wrong. This says with what, which is the question anybody who has
 * read the counts asks next — and the two answers are not the same shape. Forty pictures under
 * "refused" is either one host with hotlink protection or forty publishers each losing one,
 * and nothing but the addresses tells those apart.
 *
 * Each row carries its own Reset as well as the whole group's, because those are different
 * decisions. Resetting a category is for when *this program* changed — a decoder it did not
 * have. Resetting one address is for when a host did, and asking the other thirty-nine again
 * on the strength of it is thirty-nine requests nobody has a reason for.
 */
export function ImageListDialog({
  failure,
  onClose,
}: {
  failure: ImageFailure | null;
  onClose: () => void;
}) {
  const open = failure !== null;
  const reason = failure?.reason ?? "";
  const pictures = useUnmeasuredImages(reason, open);
  const retry = useRetryImages();

  const listed = pictures.data?.pictures ?? [];
  const name = reason || "waiting";

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={failure ? `${name} · ${failure.count}` : ""}
      wide
      flush
      footer={
        <div className="flex flex-wrap items-center gap-3">
          {/* Nothing to reset on the ones nothing has asked about: they are already due,
              and the only thing between them and a measurement is the queue's own pace. */}
          {reason ? (
            <Button
              onClick={() => retry.mutate({ reason })}
              disabled={retry.isPending}
            >
              {retry.isPending ? "Resetting…" : `Reset all ${failure?.count}`}
            </Button>
          ) : null}
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </div>
      }
    >
      {pictures.isPending ? (
        <Spinner />
      ) : pictures.error ? (
        <Alert>{pictures.error.message}</Alert>
      ) : listed.length === 0 ? (
        <p className="px-1 py-6 text-sm text-ink-muted">
          Nothing here any more — these have been asked about since the count
          was taken.
        </p>
      ) : (
        <ul className="flex flex-col">
          {listed.map((picture) => (
            <li
              key={picture.url}
              className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-rule py-3 last:border-b-0"
            >
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-ink" title={picture.title}>
                  {picture.title || "an article with no title"}
                </p>
                {/* The address itself, and a link to it. Half of deciding what a failure
                    means is looking at what the host actually answers, and retyping a CDN
                    URL out of a screenshot is not a thing anybody should have to do. */}
                <a
                  href={picture.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block truncate font-mono text-xs text-ink-faint hover:text-accent"
                  title={picture.url}
                >
                  {picture.url}
                </a>
              </div>

              <span
                className="shrink-0 text-xs text-ink-faint"
                title={picture.retry_at ? exact(picture.retry_at) : undefined}
              >
                {picture.articles > 1 ? `${picture.articles} articles · ` : ""}
                {picture.retry_at ? until(picture.retry_at) : "due"}
              </span>

              <Button
                variant="ghost"
                onClick={() => retry.mutate({ url: picture.url })}
                disabled={retry.isPending}
              >
                Reset
              </Button>
            </li>
          ))}
        </ul>
      )}

      {/* Only when the list was actually cut short, which is not the same as it being shorter
          than the count in the title. That happens constantly and means the opposite: the
          queue has measured some of them since the count was taken, and a note saying
          "showing 1 of 2" over a complete list of one reads as a fault rather than as work
          getting done. */}
      {pictures.data && listed.length >= pictures.data.limit ? (
        <p className="pt-3 text-xs text-ink-faint">
          The {listed.length} most used. There are more.
        </p>
      ) : null}

      {retry.error ? <Alert>{retry.error.message}</Alert> : null}
    </Modal>
  );
}
