import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useRef } from "react";

import { getPublicPage } from "@app/api/actions/public";
import { useApiCall } from "@app/api/provider";
import { ArticleCard } from "@app/apps/reader/ArticleCard";
import { Boundary } from "@app/components/Boundary";
import { Spinner } from "@app/components/ui/Spinner";
import { useMasonry } from "@app/lib/masonry";
import { assignVoices, styleFor } from "@app/lib/voice";

/**
 * Somebody's published page, to anybody at all.
 *
 * Its own island rather than a route in the reader, and the reason is what the reader is: an
 * application for somebody with an account, which knows about sessions and settings and marking
 * things read. A stranger opening a link should be handed a page — not the shell of a product
 * they have no account for, half of whose controls would refuse them.
 *
 * What is on it is the same page its owner reads, set the same way: the same grid, the same
 * drawn styles, the same faces. Two things are missing, and both are absences rather than
 * refusals — there is no way to compose a new one, and no way to mark anything read. A control
 * that exists and says no is still advertising what an account would let you do.
 */
export function App() {
  const path = window.location.pathname.split("/").filter(Boolean);
  // "/p/<person>/<page>" — anything shorter is a link that was cut in half, which is what
  // messaging apps do to long URLs.
  const person = path[1] ?? "";
  const page = path[2] ?? "";

  return (
    <Boundary>
      <PublicPage person={person} page={page} />
    </Boundary>
  );
}

function PublicPage({ person, page }: { person: string; page: string }) {
  const callApi = useApiCall();
  const grid = useRef<HTMLDivElement>(null);

  const published = useQuery({
    queryKey: ["public", person, page],
    queryFn: ({ signal }) => callApi(getPublicPage(person, page), signal),
    enabled: person !== "" && page !== "",
    retry: false,
  });

  useMasonry(grid, [
    published.data?.generated_at,
    published.data?.items.length,
  ]);

  // The document says noindex until the page says otherwise, and it only says otherwise when
  // the owner asked *and* the instance allows it. The default is the safe one because the
  // mistake is not reversible: a crawled page stays in somebody else's cache long after it is
  // taken down, and nothing here reaches that.
  useEffect(() => {
    if (!published.data?.indexable) return;
    const tag = document.querySelector('meta[name="robots"]');
    tag?.parentElement?.removeChild(tag);
  }, [published.data?.indexable]);

  useEffect(() => {
    if (published.data) document.title = `${published.data.name} · bystander`;
  }, [published.data]);

  const missing = person === "" || page === "" || published.isError;

  return (
    <>
      <Masthead />
      <main className="mx-auto max-w-[1400px] px-6 py-8">
        {missing ? (
          <Gone />
        ) : published.isPending ? (
          <Spinner />
        ) : published.data.items.length === 0 ? (
          <p className="py-16 text-center text-sm text-ink-muted">
            Nothing on this page yet.
          </p>
        ) : (
          <>
            <h1 className="mb-8 font-serif text-3xl text-ink">
              {published.data.name}
            </h1>
            <div className="page-grid" ref={grid}>
              {(() => {
                const items = published.data.items;
                // Seeded on the composition and the article together, exactly as the owner's
                // own page is — so a published page looks the same to everybody who opens it,
                // and the same as it does to the person who published it.
                const seed = String(published.data.generated_at);
                const styles = items.map((article) =>
                  styleFor(seed, article.id, article.summary),
                );
                const voices = assignVoices(styles);

                let ruledAt = 0;
                return items.flatMap((article, i) => {
                  // No onRead: nobody here can mark anything, and the card leaves the
                  // control out rather than showing one that refuses.
                  const card = (
                    <ArticleCard
                      key={article.id}
                      article={article}
                      style={styles[i]!}
                      voice={voices[i]!}
                    />
                  );
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
          </>
        )}
      </main>
    </>
  );
}

/**
 * The band across the top, and it is not the reader's.
 *
 * A wordmark and a way in, and nothing else. No settings, no account, no name of whoever
 * published this — the address already carries the one identity they chose to expose, and a
 * username beside it would expose one they did not.
 */
function Masthead() {
  return (
    <header className="border-b border-rule">
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-baseline gap-x-6 gap-y-1 px-6 py-5">
        <a href="/" className="nameplate text-ink hover:text-accent">
          bystander
        </a>
        <p className="basis-full text-sm text-ink-muted sm:basis-auto">
          A reader with no unread count
        </p>
        <div className="flex basis-full items-center gap-4 text-sm sm:ml-auto sm:basis-auto">
          <a href="/login" className="text-ink-muted hover:text-ink">
            Sign in
          </a>
        </div>
      </div>
    </header>
  );
}

/**
 * Everything that can go wrong says the same thing, because everything that can go wrong is
 * the same thing to whoever is holding the link: there is no page here.
 *
 * No such person, no such page, taken down, an instance that publishes nothing — a stranger has
 * no business learning which, and the owner already knows.
 */
function Gone() {
  return (
    <div className="py-20 text-center">
      <h1 className="font-serif text-2xl text-ink">No page at this address</h1>
      <p className="mx-auto mt-2 max-w-prose text-sm text-ink-muted">
        It may have been taken down, or the link may have been cut short on its
        way to you.
      </p>
    </div>
  );
}
