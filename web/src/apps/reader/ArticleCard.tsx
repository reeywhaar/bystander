import type { MouseEvent } from "react";

import type { Article } from "@app/api/types";
import { exact, since } from "@app/lib/time";
import { columnsFor, type Style, type Voice } from "@app/lib/voice";

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
  style,
  voice,
  onRead,
}: {
  article: Article;
  /** Everything about how this card looks, drawn from the edition and the article. */
  style: Style;
  /** The face, which is the one thing the page decides rather than the card. */
  voice: Voice;
  onRead: (id: string, read: boolean) => void;
}) {
  const read = article.read_at !== null;
  // Null for most of them: a box is punctuation, and the padding belongs to the box rather
  // than to every card that might have had one.
  const frame = style.frame;
  const frameClass = frame
    ? `frame frame-${frame.line} frame-width-${frame.width} frame-ink-${frame.ink} frame-pad-${frame.pad}`
    : "";
  const showImage = article.image_url !== "" && article.slot !== "brief";
  // Zero means nothing has measured it, which is the ordinary case for anything published in
  // the last few minutes. See internal/jobs.
  const measured = article.image_width > 0 && article.image_height > 0;
  const showSummary = article.summary !== "" && article.slot !== "brief";

  // The picture beside the story rather than above it.
  //
  // Only on boxed cards, and only where there is an edge for it to sit against — a picture
  // half the width of a card needs the frame, or it reads as one that failed to be full width.
  // Never the lead: that one runs the width of the page and its picture is the page's opening
  // image, which is a different job from illustrating a column.
  const aside =
    showImage && frame !== null && style.aside && article.slot !== "lead";

  // A picture beside the story has taken the width the columns would have been set in, so the
  // body is a single column whatever it drew. Two things competing for one measure is how a
  // card ends up with neither.
  const columns = aside ? 1 : columnsFor(article.slot, style.columns);

  return (
    <article
      className={`slot-${article.slot} ${
        aside ? "card-aside" : "flex flex-col"
      } ${frameClass} ${read ? "is-read" : ""}`}
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
            // The lead keeps a shape of its own. It runs the width of the page, so a crop
            // from the ladder would put a picture three quarters of a screen tall above the
            // story it belongs to — the cinematic ratio is what keeps a full-width picture
            // from becoming the page.
            // Written out rather than built from `style.fit`. Tailwind generates a class
            // only if it can find the whole name in the source, so `object-${fit}` produced
            // neither — the images had no object-fit at all and were stretching to their
            // box. A template literal is invisible to it.
            // A measured picture is always filled, never fitted.
            //
            // Its box is its own ratio, so for nearly all of them `contain` and `cover` draw
            // exactly the same thing. They differ only on a picture too wide or too tall for
            // this page, whose box has been squared — and there `contain` would letterbox a
            // panorama into a square hole, which is the shape the squaring was avoiding.
            // Filling takes the middle of the picture instead.
            //
            // The fit still varies on pictures nothing has measured, where the box is a
            // guess and showing one whole is as good an answer as cropping it.
            className={`w-full rounded-sm border border-rule ${aside ? "" : "mb-3"} ${
              !measured && style.fit === "contain"
                ? "object-contain"
                : "object-cover"
            } ${
              article.slot === "lead"
                ? "aspect-[21/9]"
                : measured
                  ? ""
                  : `shot-${style.shot}`
            }`}
            // The picture's own shape, when anything has measured it.
            //
            // The drawn ladder is what to do knowing nothing, and it is a good answer for
            // that — but a photograph that is nearly square cut to five-by-three loses a
            // third of itself, and nobody here has looked at it to know whether the third
            // mattered. A measurement replaces the guess with the fact.
            //
            // Bounded rather than clamped, and the difference is the point — see [shapeOf].
            style={
              measured
                ? {
                    aspectRatio: shapeOf(
                      article.image_width / article.image_height,
                    ),
                  }
                : undefined
            }
            // A publisher's image that 404s or hotlink-blocks would otherwise leave a
            // broken-image glyph in the middle of the page.
            onError={(event) => {
              event.currentTarget.style.display = "none";
            }}
          />
        </a>
      ) : null}

      {/* Everything that is not the picture, as one block — so a card is two children and
          the layout is only a question of direction. `flex-1` and `min-w-0` are for the row
          only: `flex-basis: 0` in a column would make the body's hypothetical height zero,
          which is the shape a collapsing auto-height flex container is made of. */}
      <div
        className={`card-body flex flex-col ${aside ? "min-w-0 flex-1" : ""}`}
      >
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
            // No size utility, for the same reason the headline has none: the size is this
            // story's own, from a ladder in styles.css, and a `text-base` here would win the
            // cascade and flatten it. Larger than it began, too — the standfirst is the only
            // prose on the page; everything else here is scanned, this is read.
            className={`prose-summary prose-step-${style.prose} mt-2 text-ink-muted ${
              // The drawn number, held to what this slot is wide enough to carry — see
              // columnsFor. One column is not a split, so it gets no class at all.
              columns > 1 ? `prose-columns prose-columns-${columns}` : ""
            }`}
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
        {/* Close to its own story, not floating between two.
          
          Cards hug their content now, so the only thing under this control is the grid's
          gap and then the next card — and at twelve pixels above against twenty-eight
          below it read as belonging to neither. Six is unambiguous: it is nearer to the
          article it marks than that article is to anything else. */}
        <div className="pt-1.5">
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
      </div>
    </article>
  );
}

/**
 * The widest and tallest a measured picture is drawn at, as width over height.
 *
 * Two and a half to one either way — a wide landscape through square to a tall portrait. Far
 * looser than the drawn ladder, which only ever runs from five-by-three to square, and
 * deliberately so: the ladder is what to do knowing nothing, and a measurement is not nothing.
 * A photograph that is genuinely a panorama should look like one, and a portrait should stand
 * up. Holding every measured picture inside the guess's range would be measuring them and then
 * declining to believe the answer.
 */
const WIDEST = 5 / 2;
const TALLEST = 2 / 5;

/**
 * The shape a measured picture is drawn in.
 *
 * Inside the bounds a picture is drawn at its own ratio, whatever that is. Outside them it is
 * drawn square and filled, which crops it — and square rather than the nearest bound, which is
 * what this used to do.
 *
 * That difference is worth being clear about, because clamping sounds like the gentler of the
 * two and is not. A 4:1 publisher's banner clamped to the widest allowed shape is still very
 * nearly a 4:1 banner: it keeps the letterbox proportions that made it wrong for a column of
 * stories, and only stops just short of them. Squaring it admits the page has no room for that
 * shape at all and takes the middle of the picture instead. The same holds for the tall end,
 * where clamping leaves a product shot still tall enough to push its own headline off screen.
 *
 * So the bounds are not a range to be squeezed into. They are the range this page can carry
 * honestly, and a picture outside it is cropped rather than distorted towards the edge.
 */
function shapeOf(ratio: number): number {
  // Square, and [ArticleCard] fills rather than fits every measured picture, so the crop
  // takes the middle instead of letterboxing what would not go.
  if (ratio > WIDEST || ratio < TALLEST) return 1;
  return ratio;
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
