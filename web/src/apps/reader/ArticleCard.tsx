import type { Article } from "@app/api/types";
import { exact, since } from "@app/lib/time";

/**
 * One article, in the slot the server gave it.
 *
 * The slot decides the shape and nothing here recomputes it — no measurement, no layout
 * pass, so the page does not reflow after paint and two loads of one edition are identical.
 */
export function ArticleCard({
  article,
  onRead,
}: {
  article: Article;
  onRead: (id: string, read: boolean) => void;
}) {
  const read = article.read_at !== null;
  const big = article.slot === "lead" || article.slot === "feature";
  const showImage = article.image_url !== "" && article.slot !== "brief";
  const showSummary = article.summary !== "" && article.slot !== "brief";

  return (
    <article
      className={`slot-${article.slot} group flex flex-col ${read ? "is-read" : ""} transition-[opacity,filter] duration-200`}
    >
      {showImage ? (
        <a
          href={article.link}
          target="_blank"
          rel="noopener noreferrer"
          onClick={() => onRead(article.id, true)}
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

      <h2
        className={`font-serif leading-tight text-ink ${
          article.slot === "lead"
            ? "text-4xl sm:text-5xl"
            : article.slot === "feature"
              ? "text-2xl"
              : "text-lg"
        }`}
      >
        <a
          href={article.link}
          target="_blank"
          rel="noopener noreferrer"
          onClick={() => onRead(article.id, true)}
          className="hover:text-accent hover:underline underline-offset-4"
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

      <div className="mt-auto pt-3">
        <button
          type="button"
          onClick={() => onRead(article.id, !read)}
          className="text-xs text-ink-faint opacity-0 transition-opacity group-hover:opacity-100
            focus-visible:opacity-100 hover:text-ink"
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
          className="hover:text-accent"
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
