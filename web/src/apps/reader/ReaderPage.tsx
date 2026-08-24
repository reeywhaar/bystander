import { useEffect, useRef } from "react";
import { useParams } from "react-router";

import { ApiError } from "@app/api/error";
import type { Me, Page } from "@app/api/types";
import { Masthead } from "@app/components/Masthead";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute, until } from "@app/lib/time";
import { useMasonry } from "@app/lib/masonry";
import { assignVoices, styleFor } from "@app/lib/voice";
import {
  useEdition,
  useFeeds,
  usePages,
  useRegenerate,
  useSetRead,
} from "@app/queries/hooks";

import { ArticleCard } from "@app/apps/reader/ArticleCard";
import { PageTabs } from "@app/apps/reader/PageTabs";

export function ReaderPage({ me }: { me: Me }) {
  // Which front page this is. Absent at the root, which is the main page — and which the
  // endpoints already read as "the main one", so nothing here has to special-case it.
  const { slug = "" } = useParams();

  const edition = useEdition(slug);
  const setRead = useSetRead();
  const regenerate = useRegenerate(slug);
  const resetCompose = regenerate.reset;
  // Which page this is, for the empty state: a page filtered to nothing is empty for a
  // completely different reason from an account with no feeds.
  const pages = usePages();

  // Moving to another tab is arriving at a different front page, and nothing about the last
  // one should survive it.
  //
  // Both routes render this same component, so React keeps the instance and hands it new
  // props rather than remounting — which is what makes the two things below necessary rather
  // than automatic. "Everything here has been read" is about the page it was said of, and it
  // sat there over the top of a page nobody had composed yet. And the scroll position belongs
  // to the page being left; carrying it over drops somebody into the middle of one they have
  // not seen the top of.
  useEffect(() => {
    resetCompose();
    window.scrollTo({ top: 0 });
  }, [slug, resetCompose]);
  // Only to tell the two empty states apart: somebody with no feeds needs a different
  // sentence from somebody whose feeds have not been fetched yet.
  const feeds = useFeeds();

  // Above the early returns, because hooks cannot be called conditionally. It does nothing
  // until the grid is on the page.
  const grid = useRef<HTMLDivElement>(null);
  // The page id is in here because moving between tabs replaces every card without unmounting
  // the grid, and a fresh set of cards is a fresh set of heights to measure.
  useMasonry(grid, [edition.data?.id, edition.data?.items.length]);

  if (edition.isPending) return <Spinner label="Fetching your front page" />;
  if (edition.error) throw edition.error;

  const page = edition.data;
  const hasPage = page.items.length > 0;

  return (
    <>
      <Masthead
        me={me}
        subtitle={
          hasPage
            ? `${absolute(page.generated_at)} · ${page.items.length} article${page.items.length === 1 ? "" : "s"}`
            : undefined
        }
      >
        <span className="hidden text-ink-faint sm:inline">
          Next page {until(page.next_edition_at)}
        </span>
      </Masthead>

      <PageTabs />

      <main className="mx-auto max-w-[1400px] px-6 py-10">
        {regenerate.error ? (
          <div className="mb-6">
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

        {hasPage ? (
          <div className="page-grid" ref={grid}>
            {/* One seeded stream per card, keyed on the edition and the article together —
                so the page is identical on reload and different tomorrow. The voices are
                then settled over the whole page, because "no two headlines in a row share a
                face" is a fact about the sequence and a card cannot see it. */}
            {(() => {
              const styles = page.items.map((article) =>
                styleFor(page.id, article.id, article.summary),
              );
              const voices = assignVoices(styles);

              // A rule spans every track, so a card caught between two of them sits alone
              // with three quarters of its row empty. The draw is per card and cannot know
              // where the last rule fell, so the run enforces the floor — the same shape as
              // the no-two-faces-in-a-row rule, and for the same reason: it is a fact about
              // the sequence, not about any one card.
              let ruledAt = 0;

              return page.items.flatMap((article, i) => {
                const card = (
                  <ArticleCard
                    key={article.id}
                    article={article}
                    style={styles[i]!}
                    voice={voices[i]!}
                    onRead={(id, read) => setRead.mutate({ id, read })}
                  />
                );
                // A rule above the cards that drew one, so the page reads as bands rather
                // than as one field. Never above the first — a page does not open with a
                // rule over its lead — and never within four cards of the last one.
                if (i > 0 && styles[i]!.rule && i - ruledAt >= 4) {
                  ruledAt = i;
                  return [
                    <hr key={`rule-${article.id}`} className="page-rule" />,
                    card,
                  ];
                }
                return [card];
              });
            })()}
          </div>
        ) : (
          <EmptyPage
            hasFeeds={(feeds.data?.length ?? 0) > 0}
            filtered={isFiltered(
              (pages.data ?? []).find((p) =>
                slug ? p.slug === slug : p.is_main,
              ),
            )}
            onCompose={() => regenerate.mutate()}
            composing={regenerate.isPending}
          />
        )}

        {hasPage ? (
          <footer className="mt-16 flex flex-wrap items-end justify-between gap-4 border-t border-rule pt-6 text-sm text-ink-muted">
            <p className="max-w-lg">
              The next page is due {until(page.next_edition_at)}. When it
              arrives, this one is gone for good — articles and read marks
              alike. What you have{" "}
              <a
                href="/manage/read"
                className="underline underline-offset-2 hover:text-ink"
              >
                already read
              </a>{" "}
              is kept for a month.
            </p>
            {/* Left-aligned when it wraps under the paragraph, right-aligned when it sits
                beside it. `items-end` alone left the button floating at an indent that
                matched nothing on the page. */}
            <div className="flex flex-col items-start gap-1 sm:items-end">
              <Button
                onClick={() =>
                  regenerate.mutate(undefined, {
                    // Whatever came of it, the thing to look at is at the top and this
                    // button is at the bottom.
                    //
                    // On success the page underneath has been replaced entirely, and
                    // staying put would land somebody in the middle of one they have not
                    // seen the top of. On a refusal — everything here read, nothing new
                    // published — the sentence saying so is also at the top, and this used
                    // to scroll only on success: you pressed the button, nothing appeared
                    // to happen, and the explanation was several screens above you.
                    onSettled: () => window.scrollTo({ top: 0 }),
                  })
                }
                disabled={regenerate.isPending}
              >
                {regenerate.isPending ? "Composing…" : "Make a different page"}
              </Button>
              {/* Worth saying, because the sentence beside it says the opposite about the
                  scheduled turn — and somebody who believes this button burns their feeds
                  will never press it. */}
              <span className="text-xs text-ink-faint">
                Nothing you have not read is lost
              </span>
            </div>
          </footer>
        ) : null}
      </main>
    </>
  );
}

/** Whether a page draws from less than everything. */
function isFiltered(page: Page | undefined): boolean {
  return Boolean(
    page && (page.tag_filter !== "no" || page.feed_filter !== "all"),
  );
}

/**
 * What somebody sees when a page has nothing on it.
 *
 * Three situations, and they need three different sentences — the point being that only one of
 * them is a problem and the other two are ordinary.
 *
 * With no feeds there is nothing to do but add one. With feeds but no page — the ordinary state
 * for the first minute or two, while the poller fetches — what they want is to stop waiting, so
 * the button belongs right here rather than in a footer that only appears once there is a page
 * to sit under. And a filtered page with nothing on it has feeds and is not waiting: its filter
 * matches nothing, which is a thing to go and widen rather than a thing to compose again.
 */
function EmptyPage({
  hasFeeds,
  filtered,
  onCompose,
  composing,
}: {
  hasFeeds: boolean;
  filtered: boolean;
  onCompose: () => void;
  composing: boolean;
}) {
  if (hasFeeds && filtered) {
    return (
      <div className="mx-auto max-w-lg py-20 text-center">
        <h1 className="font-serif text-3xl text-ink">Nothing matches</h1>
        <p className="mt-3 text-ink-muted">
          Nothing you follow fits what this page draws from — or nothing that
          does has published lately. Widen it, or give it time.
        </p>
        <div className="mt-6">
          <a
            href="/manage/pages"
            className="inline-flex rounded-md bg-ink px-4 py-2 text-sm font-medium text-paper hover:bg-ink/85"
          >
            Edit this page
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-lg py-20 text-center">
      <h1 className="font-serif text-3xl text-ink">Nothing on the page yet</h1>

      {hasFeeds ? (
        <>
          <p className="mt-3 text-ink-muted">
            Your feeds are being fetched. A page will be composed on its own
            shortly — or make one now.
          </p>
          <div className="mt-6">
            <Button variant="primary" onClick={onCompose} disabled={composing}>
              {composing ? "Composing…" : "Make my first page"}
            </Button>
          </div>
        </>
      ) : (
        <>
          <p className="mt-3 text-ink-muted">
            Add a few feeds and bystander will compose a front page from them.
            There is no unread count, and there never will be — when the next
            page is made, this one is gone.
          </p>
          <div className="mt-6">
            <a
              href="/manage"
              className="inline-flex rounded-md bg-ink px-4 py-2 text-sm font-medium text-paper hover:bg-ink/85"
            >
              Add your first feed
            </a>
          </div>
        </>
      )}
    </div>
  );
}
