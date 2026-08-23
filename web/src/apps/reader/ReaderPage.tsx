import { ApiError } from "@app/api/error";
import type { Me } from "@app/api/types";
import { Masthead } from "@app/components/Masthead";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute, until } from "@app/lib/time";
import { useEdition, useRegenerate, useSetRead } from "@app/queries/hooks";

import { ArticleCard } from "@app/apps/reader/ArticleCard";

export function ReaderPage({ me }: { me: Me }) {
  const edition = useEdition();
  const setRead = useSetRead();
  const regenerate = useRegenerate();

  if (edition.isPending) return <Spinner label="Fetching your page" />;
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
          <div className="page-grid">
            {page.items.map((article) => (
              <ArticleCard
                key={article.id}
                article={article}
                onRead={(id, read) => setRead.mutate({ id, read })}
              />
            ))}
          </div>
        ) : (
          <EmptyPage />
        )}

        <footer className="mt-16 flex flex-wrap items-center justify-between gap-4 border-t border-rule pt-6 text-sm text-ink-muted">
          <p>
            The next page is due {until(page.next_edition_at)}. When it arrives,
            this one is gone for good.
          </p>
          <Button
            onClick={() => regenerate.mutate()}
            disabled={regenerate.isPending}
          >
            {regenerate.isPending ? "Composing…" : "Make a new page now"}
          </Button>
        </footer>
      </main>
    </>
  );
}

/**
 * What a brand new account sees.
 *
 * "No page yet" and "your feeds have published nothing" are not distinguished here,
 * because before a feed exists only one of them is possible and after one exists the
 * regeneration button says the other in the server's own words.
 */
function EmptyPage() {
  return (
    <div className="mx-auto max-w-lg py-20 text-center">
      <h1 className="font-serif text-3xl text-ink">Nothing on the page yet</h1>
      <p className="mt-3 text-ink-muted">
        Add a few feeds and bystander will compose a front page from them. There
        is no unread count, and there never will be — when the next page is
        made, this one is gone.
      </p>
      <div className="mt-6">
        <a
          href="/manage"
          className="inline-flex rounded-md bg-ink px-4 py-2 text-sm font-medium text-paper hover:bg-ink/85"
        >
          Add your first feed
        </a>
      </div>
    </div>
  );
}
