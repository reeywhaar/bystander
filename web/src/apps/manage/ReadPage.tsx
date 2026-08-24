import type { ReadArticle } from "@app/api/types";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute, exact, since } from "@app/lib/time";
import { useReadArticles } from "@app/queries/hooks";

/**
 * What has been read, most recent first.
 *
 * The one list in this application that is allowed to exist, because it counts nothing and
 * asks for nothing: everything on it is already dealt with. It is here rather than in the
 * reader because it is a thing to look back at, not the front page — and keeping it out of
 * the reader's bundle keeps that bundle to the page itself.
 *
 * The record behind it is kept for as long as the feed is followed, because it is also what
 * stops an article somebody has read being offered again. The list is the bounded half — the
 * server sends the most recent few hundred — which is what "recently" is doing in the name.
 */
export function ReadPage() {
  const articles = useReadArticles();

  if (articles.isPending) return <Spinner />;
  if (articles.error) throw articles.error;

  if (articles.data.length === 0) {
    return (
      <p className="py-16 text-center text-sm text-ink-muted">
        Nothing yet. Articles you mark read turn up here, and stay for as long
        as you follow the feed they came from.
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-8">
      <p className="text-sm text-ink-muted">
        Kept for as long as you follow the feed. Pages come and go; this
        outlives them — and it is what keeps an article you have finished with
        from turning up on a later one.
      </p>

      {group(articles.data).map(([day, entries]) => (
        <section key={day}>
          <h2 className="mb-2 border-b border-rule pb-1 text-xs tracking-wide text-ink-faint uppercase">
            {day}
          </h2>
          <ul className="flex flex-col">
            {entries.map((article) => (
              <li
                key={article.item_id}
                className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 py-2"
              >
                <a
                  href={article.link}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-serif text-base text-ink hover:underline underline-offset-4"
                >
                  {article.title}
                </a>
                <span className="text-xs text-ink-faint">
                  {article.feed.title || "no longer followed"}
                </span>
                <time
                  className="ml-auto text-xs text-ink-faint"
                  dateTime={new Date(article.read_at * 1000).toISOString()}
                  title={exact(article.read_at)}
                >
                  {since(article.read_at)}
                </time>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

/**
 * Groups by the day it was read, preserving the server's newest-first order.
 *
 * A Map rather than an object, because insertion order is what keeps the days in sequence —
 * an object with date-shaped keys is at the mercy of how the engine orders them.
 */
function group(articles: ReadArticle[]): [string, ReadArticle[]][] {
  const days = new Map<string, ReadArticle[]>();
  for (const article of articles) {
    const day = dayOf(article.read_at);
    const existing = days.get(day);
    if (existing) existing.push(article);
    else days.set(day, [article]);
  }
  return [...days];
}

function dayOf(unix: number): string {
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const start = midnight.getTime() / 1000;

  if (unix >= start) return "Today";
  if (unix >= start - 86_400) return "Yesterday";
  return absolute(unix);
}
