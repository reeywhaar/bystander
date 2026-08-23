import { ApiError } from "@app/api/error";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Slider } from "@app/components/ui/Slider";
import { Spinner } from "@app/components/ui/Spinner";
import {
  ARTICLE_WINDOWS,
  EDITION_INTERVALS,
  EDITION_SIZE,
} from "@app/lib/constants";
import { until } from "@app/lib/time";
import {
  useRegenerate,
  useSettings,
  useUpdateSettings,
} from "@app/queries/hooks";

export function SettingsPage() {
  const settings = useSettings();
  const update = useUpdateSettings();
  const regenerate = useRegenerate();

  if (settings.isPending) return <Spinner />;
  if (settings.error) throw settings.error;

  const current = settings.data;

  return (
    <div className="flex flex-col gap-10">
      <section>
        <h2 className="font-serif text-xl text-ink">
          How often a new page is made
        </h2>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          When the next page arrives, the one before it is gone for good —
          articles and read marks alike. The next is due{" "}
          {until(current.next_edition_at)}.
        </p>

        <div className="flex flex-wrap gap-2">
          {EDITION_INTERVALS.map((interval) => {
            const on = interval.seconds === current.edition_interval;
            return (
              <button
                key={interval.seconds}
                type="button"
                onClick={() =>
                  update.mutate({ edition_interval: interval.seconds })
                }
                disabled={update.isPending}
                className={`rounded-md border px-3 py-2 text-sm ${
                  on
                    ? "border-accent bg-accent/10 text-accent"
                    : "border-rule text-ink-muted hover:text-ink"
                }`}
              >
                {interval.label}
              </button>
            );
          })}
        </div>
      </section>

      <section>
        <h2 className="font-serif text-xl text-ink">How much is on it</h2>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          A ceiling, not a quota. If your feeds have published less than this,
          the page is shorter — it is never padded with things you have already
          seen.
        </p>

        <Slider
          value={current.edition_size}
          min={EDITION_SIZE.min}
          max={EDITION_SIZE.max}
          step={EDITION_SIZE.step}
          onCommit={(size) => update.mutate({ edition_size: size })}
          label="Articles on a page"
          format={(size) => (
            <span className="inline-block w-24 text-left text-ink sm:text-right">
              {size} articles
            </span>
          )}
        />
      </section>

      <section>
        <h2 className="font-serif text-xl text-ink">How far back it reaches</h2>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          Articles older than this are not picked. A front page is about what is
          going on, and a fortnight-old article on one is a different kind of
          object — but a feed that publishes monthly needs a longer reach to
          appear at all.
        </p>

        <div className="flex flex-wrap gap-2">
          {ARTICLE_WINDOWS.map((window) => {
            const on = window.seconds === current.article_window;
            return (
              <button
                key={window.seconds}
                type="button"
                onClick={() =>
                  update.mutate({ article_window: window.seconds })
                }
                disabled={update.isPending}
                className={`rounded-md border px-3 py-2 text-sm ${
                  on
                    ? "border-accent bg-accent/10 text-accent"
                    : "border-rule text-ink-muted hover:text-ink"
                }`}
              >
                {window.label}
              </button>
            );
          })}
        </div>
      </section>

      <section>
        <h2 className="font-serif text-xl text-ink">Make a page now</h2>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          Rather than waiting for the next one. Priorities are probabilities, so
          composing again gives a different arrangement of what your feeds have
          published — press it as often as you like while you settle on them.
          Nothing you have not read is lost; only the scheduled page turn spends
          what it shows.
        </p>

        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="primary"
            onClick={() => regenerate.mutate()}
            disabled={regenerate.isPending}
          >
            {regenerate.isPending ? "Composing…" : "Compose a page"}
          </Button>
          {regenerate.isSuccess && !regenerate.isPending ? (
            <a
              href="/"
              className="text-sm text-accent underline underline-offset-2"
            >
              Ready — go and read it
            </a>
          ) : null}
        </div>

        {regenerate.error ? (
          <div className="mt-3">
            {/* A conflict is a statement about the world — everything read, nothing new —
                rather than something that went wrong, and it should not look alarming. */}
            <Alert
              tone={
                regenerate.error instanceof ApiError &&
                regenerate.error.conflict
                  ? "note"
                  : "error"
              }
            >
              {regenerate.error.message}
            </Alert>
          </div>
        ) : null}
      </section>

      {update.error ? <Alert>{update.error.message}</Alert> : null}
    </div>
  );
}
