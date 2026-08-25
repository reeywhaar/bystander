import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import type { PublicPage } from "@app/api/types";
import { getPublicPage } from "@app/api/actions/public";
import { SignInDialog } from "@app/apps/public/SignInDialog";
import { useApiCall } from "@app/api/provider";
import { ArticleCard } from "@app/apps/reader/ArticleCard";
import { Masthead as AppMasthead } from "@app/components/Masthead";
import { Boundary } from "@app/components/Boundary";
import { Colophon } from "@app/components/Colophon";
import { Spinner } from "@app/components/ui/Spinner";
import { useMasonry } from "@app/lib/masonry";
import { useMe, useSetRead } from "@app/queries/hooks";
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

  // Who is looking, if anybody. The page itself already says whether the request carried a
  // session — this is for the masthead, which should look like the rest of the application to
  // somebody who has an account here rather than inviting them to sign in again.
  const me = useMe();
  const [signingIn, setSigningIn] = useState(false);
  const cacheKey = ["public", person, page];
  const published = useQuery({
    queryKey: cacheKey,
    queryFn: ({ signal }) => callApi(getPublicPage(person, page), signal),
    enabled: person !== "" && page !== "",
    retry: false,
  });

  // The same hook the reader uses, and the same endpoint behind it. Marking something read on
  // somebody else's page records it against *you*: reading is a fact about a person and an
  // article, and whose page it was seen on does not come into it. So it also greys the article
  // wherever it sits on your own pages, which is the rule that already held between two of
  // your own.
  const setRead = useSetRead();
  const client = useQueryClient();

  // Greyed the moment it is pressed, as it is on the reader. The shared hook writes its
  // optimistic update against the reader's own cache, which this page does not share — so the
  // same courtesy is done here rather than left to a round trip. The gesture is "I have
  // finished with this one", and a card that waits before greying makes it feel like a
  // request rather than a statement.
  const mark = (id: string, read: boolean) => {
    client.setQueryData<PublicPage>(cacheKey, (current) =>
      current
        ? {
            ...current,
            items: current.items.map((article) =>
              article.id === id
                ? {
                    ...article,
                    read_at: read ? Math.floor(Date.now() / 1000) : null,
                  }
                : article,
            ),
          }
        : current,
    );
    setRead.mutate({ id, read });
  };

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
      {me.data ? (
        <AppMasthead me={me.data} />
      ) : (
        <GuestMasthead onSignIn={() => setSigningIn(true)} />
      )}
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
                  // Offered only to somebody with an account. A stranger gets no control
                  // rather than one that refuses — the card leaves it out entirely, which
                  // is also what stops the page advertising what an account would let you
                  // do.
                  const card = (
                    <ArticleCard
                      key={article.id}
                      article={article}
                      style={styles[i]!}
                      voice={voices[i]!}
                      onRead={published.data.signed_in ? mark : undefined}
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
        {/* The one of the three placements that is doing the whole job: somebody here may
            never have seen this application, and the wordmark above leads to a sign-in they
            have no account for. */}
        <Colophon className="mt-16 border-t border-rule pt-6" />
      </main>

      <SignInDialog
        open={signingIn}
        onClose={(signedIn) => {
          setSigningIn(false);
          // Everything on screen depends on who is looking: the masthead, and whether the
          // cards carry a way to mark them read. Both are asked again rather than patched,
          // because the answer comes from the server either way.
          if (signedIn) {
            void client.invalidateQueries();
          }
        }}
      />
    </>
  );
}

/**
 * The band across the top, for somebody with no account here.
 *
 * A wordmark and a way in, and nothing else. No settings, no tagline, and no name of whoever
 * published this — the address already carries the one identity they chose to expose, and a
 * username beside it would expose one they did not.
 *
 * Somebody who *is* signed in gets the application's own masthead instead. They have an
 * account, they can mark things read here, and being invited to sign in again reads as the
 * page not knowing who they are — which it does.
 */
function GuestMasthead({ onSignIn }: { onSignIn: () => void }) {
  return (
    <header className="border-b border-rule">
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-baseline gap-x-6 gap-y-1 px-6 py-5">
        <a href="/" className="nameplate text-ink hover:text-accent">
          bystander
        </a>
        <div className="flex basis-full items-center gap-4 text-sm sm:ml-auto sm:basis-auto">
          {/* A button, not a link to the login island. Signing in here is not the errand —
              they are reading a page, and being sent away and brought back would lose their
              place in it for a gesture made in passing. */}
          <button
            type="button"
            onClick={onSignIn}
            className="text-ink-muted hover:text-ink"
          >
            Sign in
          </button>
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
