import type { RefObject } from "react";

import type { Article } from "@app/api/types";
import { ArticleCard } from "@app/apps/reader/ArticleCard";
import { assignVoices, styleFor } from "@app/lib/voice";

/**
 * A composed page, laid out.
 *
 * This is shared between the reader and a published page rather than written twice, and that
 * is the whole point of it: the two are not similar screens, they are the same page seen by
 * two people. Everything about how a card looks — its voice, its width, whether it is boxed,
 * where the rules fall — is drawn from `editionID` and the article's id, so a copy of this
 * logic that seeds itself differently produces a page with the same articles and none of the
 * same faces. That happened: the published page seeded on the composition time, which is
 * stable enough to look deliberate and has nothing to do with what the owner sees.
 *
 * So there is one of it, it takes the edition id as an argument, and neither caller is in a
 * position to get that wrong.
 */
export function PageGrid({
  editionID,
  items,
  onRead,
  onActions,
  gridRef,
}: {
  editionID: string;
  items: Article[];
  /**
   * Left out entirely when nobody may mark anything — a stranger on a published page gets no
   * control rather than one that refuses, which is also what stops the page advertising what
   * an account would let you do.
   */
  onRead?: (id: string, read: boolean) => void;
  /** Open what can be done about the feed one of them came from. */
  onActions?: (article: Article) => void;
  gridRef?: RefObject<HTMLDivElement | null>;
}) {
  // One seeded stream per card, keyed on the edition and the article together — so the page is
  // identical on reload and different tomorrow. The voices are then settled over the whole
  // page, because "no two headlines in a row share a face" is a fact about the sequence and a
  // card cannot see it.
  const styles = items.map((article) =>
    styleFor(editionID, article.id, article.summary),
  );
  const voices = assignVoices(styles);

  // A rule spans every track, so a card caught between two of them sits alone with three
  // quarters of its row empty. The draw is per card and cannot know where the last rule fell,
  // so the run enforces the floor — the same shape as the no-two-faces-in-a-row rule, and for
  // the same reason: it is a fact about the sequence, not about any one card.
  let ruledAt = 0;

  return (
    <div className="page-grid" ref={gridRef}>
      {items.flatMap((article, i) => {
        const card = (
          <ArticleCard
            key={article.id}
            article={article}
            style={styles[i]!}
            voice={voices[i]!}
            onRead={onRead}
            onActions={onActions ? () => onActions(article) : undefined}
          />
        );
        // A rule above the cards that drew one, so the page reads as bands rather than as one
        // field. Never above the first — a page does not open with a rule over its lead — and
        // never within four cards of the last one.
        if (i > 0 && styles[i]!.rule && i - ruledAt >= 4) {
          ruledAt = i;
          return [
            <hr key={`rule-${article.id}`} className="page-rule" />,
            card,
          ];
        }
        return [card];
      })}
    </div>
  );
}
