import { useEffect, useState } from "react";

import { ApiError } from "@app/api/error";
import type { Page } from "@app/api/types";
import { PageDialog } from "@app/apps/manage/PageDialog";
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
  useDeletePage,
  usePages,
  useRegenerate,
  useUpdatePage,
} from "@app/queries/hooks";

/** Where a page is read, which is also how it is addressed everywhere else. */
function addressOf(page: Page): string {
  return page.is_main ? "/" : `/f/${page.slug}`;
}

export function PagesPage() {
  const pages = usePages();
  const remove = useDeletePage();

  const [selected, setSelected] = useState<string | null>(null);
  const [editing, setEditing] = useState<Page | null>(null);
  const [dialog, setDialog] = useState(false);

  const all = pages.data ?? [];
  // The main page until somebody chooses otherwise, and back to it if the chosen one goes.
  const current = all.find((page) => page.id === selected) ?? all[0];

  useEffect(() => {
    if (current && current.id !== selected) setSelected(current.id);
  }, [current, selected]);

  if (pages.isPending) return <Spinner />;
  if (pages.error) throw pages.error;
  if (!current) return <Spinner />;

  return (
    <div className="flex flex-col gap-10">
      {/* One page is not a set of tabs. Somebody who has never made a second page should not
          have to look at a control for choosing between one thing. */}
      {all.length > 1 ? (
        <nav className="flex flex-wrap items-center gap-2 border-b border-rule pb-3">
          {all.map((page) => (
            <button
              key={page.id}
              type="button"
              onClick={() => setSelected(page.id)}
              aria-current={page.id === current.id ? "page" : undefined}
              className={`rounded-md px-3 py-1.5 text-sm ${
                page.id === current.id
                  ? "bg-accent/10 text-accent"
                  : "text-ink-muted hover:text-ink"
              }`}
            >
              {page.name}
            </button>
          ))}
        </nav>
      ) : null}

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-serif text-2xl text-ink">{current.name}</h2>
          <p className="mt-1 text-sm text-ink-muted">
            Read at{" "}
            <a
              href={addressOf(current)}
              className="text-accent underline underline-offset-2"
            >
              {addressOf(current)}
            </a>
            {" · "}
            {describe(current)}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button
            onClick={() => {
              setEditing(current);
              setDialog(true);
            }}
          >
            Edit
          </Button>
          <Button
            onClick={() => {
              setEditing(null);
              setDialog(true);
            }}
          >
            New page
          </Button>
          {/* The main page has no Remove, rather than one that refuses. */}
          {current.is_main ? null : (
            <Button
              variant="danger"
              onClick={() => {
                setSelected(null);
                remove.mutate(current.id);
              }}
              disabled={remove.isPending}
            >
              Remove
            </Button>
          )}
        </div>
      </header>

      {remove.error ? <Alert>{remove.error.message}</Alert> : null}

      <PageControls page={current} />

      <PageDialog
        page={editing}
        open={dialog}
        onClose={(saved) => {
          setDialog(false);
          if (saved) setSelected(saved.id);
        }}
      />
    </div>
  );
}

/** What a page draws from, in a sentence, for the line under its name. */
function describe(page: Page): string {
  const parts: string[] = [];
  const count = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"}`;

  // The tags first, because they are the funnel and the feeds are corrections to what comes
  // out of it — which is also the order somebody set them in.
  if (page.include_tag_ids.length > 0)
    parts.push(count(page.include_tag_ids.length, "tag"));
  if (page.exclude_tag_ids.length > 0)
    parts.push(
      // "all but" only when nothing narrowed it first, or the sentence would claim the page
      // draws from everything right after saying which tags it draws from.
      page.include_tag_ids.length > 0
        ? `less ${count(page.exclude_tag_ids.length, "tag")}`
        : `all but ${count(page.exclude_tag_ids.length, "tag")}`,
    );
  if (page.include_feed_ids.length > 0)
    parts.push(`always ${count(page.include_feed_ids.length, "feed")}`);
  if (page.exclude_feed_ids.length > 0)
    parts.push(`never ${count(page.exclude_feed_ids.length, "feed")}`);

  const window = ARTICLE_WINDOWS.find(
    (option) => option.seconds === page.max_article_age,
  );
  if (page.max_article_age > 0 && window) {
    parts.push(`nothing older than ${window.label.toLowerCase()}`);
  }
  return parts.length === 0 ? "everything you follow" : parts.join(", ");
}

/**
 * The three controls every page has, whichever page is being looked at.
 *
 * Outside the dialog on purpose. Turning a page from daily to hourly is one decision with one
 * obvious effect, and it takes effect the moment it is pressed; a filter is a set of choices
 * that only make sense together, which is what a modal with a save button is for.
 */
function PageControls({ page }: { page: Page }) {
  const update = useUpdatePage();
  const regenerate = useRegenerate(page.is_main ? "" : page.slug);

  const change = (changes: Parameters<typeof update.mutate>[0]["changes"]) =>
    update.mutate({ id: page.id, changes });

  return (
    <>
      <section>
        <h3 className="font-serif text-xl text-ink">
          How often a new page is made
        </h3>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          When the next page arrives, the one before it is gone for good —
          articles and read marks alike. The next is due{" "}
          {until(page.next_edition_at)}.
        </p>

        <div className="flex flex-wrap gap-2">
          {EDITION_INTERVALS.map((interval) => {
            const on = interval.seconds === page.edition_interval;
            return (
              <button
                key={interval.seconds}
                type="button"
                onClick={() => change({ edition_interval: interval.seconds })}
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
        <h3 className="font-serif text-xl text-ink">How much is on it</h3>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          A ceiling, not a quota. If your feeds have published less than this,
          the page is shorter — it is never padded with things you have already
          seen.
        </p>

        <Slider
          value={page.edition_size}
          min={EDITION_SIZE.min}
          max={EDITION_SIZE.max}
          step={EDITION_SIZE.step}
          onCommit={(size) => change({ edition_size: size })}
          label="Articles on a page"
          stacked
          format={(size) => `${size} articles`}
        />
      </section>

      <section>
        <h3 className="font-serif text-xl text-ink">How current it is</h3>
        <p className="mt-1 mb-4 text-sm text-ink-muted">
          Over the top of each feed&rsquo;s own reach, and the tighter of the
          two wins. A page about what is happening today wants a day; one you
          read at the weekend can reach back further.
        </p>

        <div className="flex flex-wrap gap-2">
          {ARTICLE_WINDOWS.map((window) => {
            const on = window.seconds === page.max_article_age;
            return (
              <button
                key={window.seconds}
                type="button"
                onClick={() => change({ max_article_age: window.seconds })}
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
        <h3 className="font-serif text-xl text-ink">Make a page now</h3>
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
              href={addressOf(page)}
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
    </>
  );
}
