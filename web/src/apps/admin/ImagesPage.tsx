import { useState } from "react";

import type { ImageFailure } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Spinner } from "@app/components/ui/Spinner";
import { useImages, useRetryImages } from "@app/queries/hooks";

import { ImageListDialog } from "@app/apps/admin/ImageListDialog";

/**
 * What each reason means, in the words somebody would use to decide what to do about it.
 *
 * The category alone is the useful thing — a hundred failures that all say "refused" is one
 * host with hotlink protection, and a hundred that say "undecodable" is a format this build
 * cannot read — but only if you know which is which without reading the source.
 */
const REASONS: Record<string, string> = {
  "": "Not asked about yet — the queue takes one every few seconds.",
  gone: "The host answered, and the picture is not there. Usually it moved.",
  refused: "The host refused. Hotlink protection, most often.",
  busy: "The host was under load or asked us to slow down. Tried again within the hour.",
  unreachable:
    "Nothing answered — a name, a connection, or the five-second wait ran out.",
  undecodable:
    "It arrived and is not a picture this build can read. AVIF and SVG land here.",
  empty: "It decoded and claimed no size at all.",
};

/**
 * How the pictures on this instance are getting on, and a way to ask again.
 *
 * A page draws a shape for a picture it has no measurements for, which looks like a design
 * choice rather than a fault — so an instance can spend months quietly cropping half its
 * comics with nothing anywhere saying so. This is the screen that says so.
 *
 * It exists because that is exactly what happened: a failure used to be permanent, and fifteen
 * of the nineteen pictures on one page were stuck behind a single bad minute at a CDN. The
 * queue retries on its own now, but "ask again now" is still the right button when the thing
 * that changed is this program rather than the host.
 */
export function ImagesPage() {
  const images = useImages();
  const retry = useRetryImages();
  // Which group's list is open, or null. The whole failure rather than its reason, because
  // the dialog's title is the count too and it would otherwise have to look it up again.
  const [open, setOpen] = useState<ImageFailure | null>(null);

  if (images.isPending) return <Spinner />;
  if (images.error) throw images.error;

  const { pictures, measured, unmeasured, failures } = images.data;

  return (
    <div className="flex flex-col gap-8">
      <section>
        <h2 className="font-serif text-xl text-ink">Pictures</h2>
        <p className="mt-1 text-sm text-ink-muted">
          Counted by picture rather than by article: one is measured once
          however many articles use it. A page draws a guessed shape for
          anything unmeasured, which reads as a choice rather than a fault — so
          the number worth watching is the second one.
        </p>

        <dl className="mt-4 flex flex-wrap gap-x-10 gap-y-3">
          <Figure label="Held" value={pictures} />
          <Figure label="Measured" value={measured} />
          <Figure label="Without a size" value={unmeasured} />
        </dl>
      </section>

      {unmeasured === 0 ? (
        <p className="text-sm text-ink-muted">
          Every picture has been measured. Nothing to do.
        </p>
      ) : (
        <section>
          <h3 className="font-serif text-lg text-ink">Why</h3>
          <p className="mt-1 mb-4 text-sm text-ink-muted">
            Open one to see which pictures are in it. Reset offers them back to
            the measuring queue straight away, and is for when this program is
            what changed — a decoder it did not have, a header it did not send.
            Pictures that were measured are never asked about again, whatever is
            pressed here.
          </p>

          <ul className="flex flex-col rounded-md border border-rule">
            {failures.map((failure) => (
              <Row
                key={failure.reason || "waiting"}
                failure={failure}
                onOpen={() => setOpen(failure)}
                onReset={() => retry.mutate({ reason: failure.reason })}
                busy={retry.isPending}
              />
            ))}
          </ul>

          <div className="mt-4 flex flex-wrap items-center gap-3">
            <Button onClick={() => retry.mutate({})} disabled={retry.isPending}>
              {retry.isPending ? "Resetting…" : "Reset all"}
            </Button>
            {retry.data ? (
              <span className="text-sm text-ink-muted">
                {retry.data.queued === 0
                  ? "Nothing to ask about again."
                  : `${retry.data.queued} queued — the measuring runs one every few seconds.`}
              </span>
            ) : null}
          </div>

          {retry.error ? <Alert>{retry.error.message}</Alert> : null}
        </section>
      )}

      <ImageListDialog failure={open} onClose={() => setOpen(null)} />
    </div>
  );
}

function Figure({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt className="text-xs text-ink-faint">{label}</dt>
      <dd className="font-serif text-2xl text-ink">{value}</dd>
    </div>
  );
}

function Row({
  failure,
  onOpen,
  onReset,
  busy,
}: {
  failure: ImageFailure;
  onOpen: () => void;
  onReset: () => void;
  busy: boolean;
}) {
  return (
    <li className="flex flex-wrap items-center gap-3 border-b border-rule last:border-b-0">
      {/* The reason and its count are the affordance, rather than a separate "show" control.
          The row is a group of pictures and the only thing anybody wants from it that is not
          already written on it is which pictures — so the row opens them. */}
      <button
        type="button"
        onClick={onOpen}
        className="min-w-0 flex-1 px-3 py-2.5 text-left hover:text-accent"
      >
        <span className="block text-sm text-ink">
          {failure.reason || "waiting"}
          {" · "}
          <span className="text-ink-muted">{failure.count}</span>
        </span>
        <span className="block text-xs text-ink-faint">
          {REASONS[failure.reason] ??
            "Something this build does not have a name for."}
        </span>
      </button>
      {/* Nothing to reset on the ones already queued: they are due, and the only thing
          between them and a measurement is the few seconds the queue takes. */}
      {failure.reason ? (
        <span className="pr-3">
          <Button variant="ghost" onClick={onReset} disabled={busy}>
            Reset
          </Button>
        </span>
      ) : null}
    </li>
  );
}
