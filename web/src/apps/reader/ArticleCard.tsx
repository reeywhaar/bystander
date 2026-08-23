import type { MouseEvent } from "react";

import type { Article } from "@app/api/types";
import { exact, since } from "@app/lib/time";
import type { Voice } from "@app/lib/voice";

/**
 * Handlers that mark an article read however it was opened.
 *
 * `onClick` covers a plain click and a modified one — cmd- or ctrl-click to open a
 * background tab both dispatch `click`. Middle click does not: browsers dispatch
 * `auxclick` for it and React's `onClick` maps only to `click`, so without this the one
 * gesture people use to open a stack of articles at once is the single way of opening one
 * that leaves it unread.
 *
 * `button === 1` because `auxclick` fires for the right button too, and opening a context
 * menu is not reading. A long-press on touch raises the menu without an auxclick at all,
 * so nothing there needs excluding.
 */
function opening(onOpen: () => void) {
  return {
    onClick: onOpen,
    onAuxClick: (event: MouseEvent<HTMLAnchorElement>) => {
      if (event.button === 1) onOpen();
    },
  };
}

/**
 * One article, in the slot the server gave it and the voice the page gave it.
 *
 * The slot decides the shape and nothing here recomputes it — no measurement, no layout
 * pass, so the page does not reflow after paint and two loads of one edition are identical.
 *
 * The voice arrives as a prop rather than being worked out from the article, because the
 * rule that no two headlines in a row share a face is a fact about the sequence and a card
 * cannot see it. See lib/voice.ts.
 */
export function ArticleCard({
  article,
  voice,
  onRead,
}: {
  article: Article;
  voice: Voice;
  onRead: (id: string, read: boolean) => void;
}) {
  const read = article.read_at !== null;
  const big = article.slot === "lead" || article.slot === "feature";
  const showImage = article.image_url !== "" && article.slot !== "brief";
  const showSummary = article.summary !== "" && article.slot !== "brief";

  return (
    <article
      className={`slot-${article.slot} flex flex-col ${read ? "is-read" : ""}`}
    >
      {showImage ? (
        <a
          href={article.link}
          target="_blank"
          rel="noopener noreferrer"
          {...opening(() => onRead(article.id, true))}
          tabIndex={-1}
          aria-hidden="true"
        >
          <img
            src={article.image_url}
            alt=""
            loading="lazy"
            className={`mb-3 w-full rounded-sm border border-rule object-cover ${
              article.slot === "lead" ? "aspect-[21/9]" : "aspect-[16/9]"
            }`}
            // A publisher's image that 404s or hotlink-blocks would otherwise leave a
            // broken-image glyph in the middle of the page.
            onError={(event) => {
              event.currentTarget.style.display = "none";
            }}
          />
        </a>
      ) : null}

      <SourceLine article={article} />

      {/* No size utility here. Both the size and the leading come from the slot and the
          voice together, in styles.css, because a face's own scale is part of what makes
          six of them look like one size — see the .headline block there. */}
      <h2 className={`headline voice-${voice} text-ink`}>
        <a
          href={article.link}
          target="_blank"
          rel="noopener noreferrer"
          {...opening(() => onRead(article.id, true))}
          className="hover:underline underline-offset-4"
        >
          {article.title}
        </a>
      </h2>

      {showSummary ? (
        <div
          className={`prose-summary mt-2 text-ink-muted ${big ? "text-base" : "text-sm"}`}
          // Sanitized on the server, at ingest, once — an allowlist of a dozen tags with
          // every script and every attribute but a resolved href removed. It is not
          // sanitized again here on purpose: a second sanitizer is a second thing to be
          // wrong, and the safe form is what is stored. See internal/feeds/sanitize.go.
          dangerouslySetInnerHTML={{ __html: article.summary }}
        />
      ) : null}

      {/* Directly under the article, not `mt-auto`. A grid cell stretches to the height of
          the tallest card in its row, so pinning this to the bottom left it stranded an
          inch below a short summary with nothing in between — tidy in a mockup where every
          card is the same length, and a hunt for the control on a real page. */}
      <div className="pt-3">
        <button
          type="button"
          onClick={() => onRead(article.id, !read)}
          // Always there, and dim. A control that exists only while the pointer is over it
          // cannot be found by somebody who does not already know it is there, and cannot
          // be reached by touch at all. Half weight keeps it out of the way of the reading
          // without hiding it, and it does not brighten — nothing on this page moves.
          className="text-xs text-ink-faint opacity-50"
        >
          {read ? "Mark unread" : "Mark read"}
        </button>
      </div>
    </article>
  );
}

function SourceLine({ article }: { article: Article }) {
  const name = article.feed.title || "Unknown source";
  return (
    <p className="mb-1.5 flex items-baseline gap-2 text-xs tracking-wide text-ink-faint uppercase">
      {article.feed.site_url ? (
        <a
          href={article.feed.site_url}
          target="_blank"
          rel="noopener noreferrer"
          className="hover:underline"
        >
          {name}
        </a>
      ) : (
        // A feed that declares no site link — the Go Blog's is exactly that — gets its
        // name without one, rather than a link that downloads an XML file.
        <span>{name}</span>
      )}
      <time
        dateTime={new Date(article.published_at * 1000).toISOString()}
        title={exact(article.published_at)}
      >
        {since(article.published_at)}
      </time>
    </p>
  );
}
