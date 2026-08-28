import { useEffect, useRef, useState, type ReactNode } from "react";

import { Colophon } from "@app/components/Colophon";
import { GitHubIcon } from "@app/components/icons/GitHubIcon";
import { GuestMasthead } from "@app/components/GuestMasthead";
import { SignInDialog } from "@app/components/SignInDialog";
import { PRODUCT } from "@app/lib/constants";
import type { Voice } from "@app/lib/voice";

/**
 * What a stranger gets at "/".
 *
 * This document is what the server hands anybody arriving without a session — see shellFor in
 * internal/api/spa.go, where "/" is the one route that reads the request. Before it existed
 * they got the reader's shell, a bundle they cannot use, a 401 and a redirect to a login form,
 * without a word about what they were looking at on the way.
 *
 * It argues rather than lists. The one thing worth saying about this reader is a claim about
 * what it refuses to do, and a feature grid is the wrong shape for a claim — so the page opens
 * with the refusal, explains what it buys, and only then says what is in it.
 */
export function LandingPage() {
  const [signingIn, setSigningIn] = useState(false);

  return (
    <>
      <GuestMasthead onSignIn={() => setSigningIn(true)} source />

      <main className="mx-auto max-w-[1100px] px-6 py-16 sm:py-24">
        <Reveal>
          <h1 className="headline voice-didone max-w-[18ch] text-4xl text-ink sm:text-6xl">
            An RSS reader with no unread count.
          </h1>
          <p className="mt-4 text-sm text-ink-faint">
            by{" "}
            <a
              href={PRODUCT.author.url}
              target="_blank"
              rel="noopener noreferrer"
              className="underline underline-offset-2 hover:text-ink"
            >
              {PRODUCT.author.name}
            </a>
          </p>
          <p className="prose-summary mt-8 max-w-2xl text-lg text-ink-muted">
            It fetches your feeds on a schedule and composes a{" "}
            <span className="text-ink">front page</span> from them — a fixed set
            of articles, in fixed positions, laid out like a newspaper. When the
            next page is made, the previous one is gone for good.
          </p>
          <p className="prose-summary mt-4 max-w-2xl text-lg text-ink-muted">
            Nothing accumulates. Nothing is owed. A feed that publishes forty
            items a day contributes the same handful as one that publishes two.
          </p>

          {/* Free software you run yourself, said at the top rather than at the bottom.
              
              It used to be the last sentence on the page, under "Getting in", where it read
              as a footnote about licensing. It is not a footnote: for most of the people who
              get this far it is the *offer* — this instance is somebody else's and they
              cannot have an account on it, so the thing they can actually do is take the
              whole of it and run their own. A page whose only clear action is a sign-in
              button nobody can use has no clear action at all.
              
              The button is a link, and deliberately: it leaves for somewhere else, and
              anything that can be opened in a new tab or copied should be a thing the
              browser knows is an address. */}
          <div className="mt-8 flex flex-wrap items-center gap-x-5 gap-y-3">
            <a
              href={PRODUCT.url}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2 rounded-md bg-ink px-4 py-2.5 text-sm
                text-paper hover:bg-accent"
            >
              <GitHubIcon className="text-base" />
              Read the source on GitHub
            </a>
            <p className="text-sm text-ink-muted">
              Self-hosted, and free to run — one container and one required
              setting.
            </p>
          </div>
        </Reveal>

        <Reveal>
          <Shot
            src="/landing/frontpage.webp"
            alt="A front page: a lead article with a picture, two features beside it, and a grid of shorter items below."
            className="mt-14"
          />
        </Reveal>

        <Reveal>
          <Section title="Why" voice="antique">
            <p>
              Every reader I have used turns reading into bookkeeping. A number
              goes up, and the only way to make it go down is to look at
              everything — so the feeds you love get skimmed alongside the feeds
              you kept out of habit, and eventually you stop opening the app,
              because it has become a chore with a scoreboard.
            </p>
            <p>
              This one has no scoreboard. It cannot tell you what you missed,
              because it does not keep what you missed. You get a page. You read
              what interests you. Tomorrow you get a different page.
            </p>
          </Section>
        </Reveal>

        <Reveal>
          <Section title="What it does differently" voice="slab">
            <Difference title="A page, not a queue">
              Articles arrive in fixed positions decided when the page was made,
              and stay there. Where something sits is how you remember where you
              were — so nothing reorders under you, and marking something read
              greys it in place rather than removing it.
            </Difference>
            <Difference title="Volume buys nothing">
              Each feed gets a share of the page set by its own priority. A
              publisher posting two hundred times a day is given exactly what
              one posting twice is, at the same setting — so following a wire
              service costs you nothing.
            </Difference>
            <Difference title="Priority is a share, not an order">
              A feed at 90 gets nine times the room of one at 10, and neither is
              ever silenced. Zero means never, and it is the only thing that
              does.
            </Difference>
            <Difference title="It forgets on purpose">
              A page is replaced, not archived. What you have actually read is
              kept under Recently read for as long as you follow the feed it
              came from — a list of things you are finished with, which asks
              nothing of you.
            </Difference>
          </Section>
        </Reveal>

        <Reveal>
          <Section title="What is in it" voice="gothic" wide>
            <ul className="grid list-disc gap-x-10 gap-y-4 pl-5 sm:grid-cols-2 lg:grid-cols-3">
              <Feature>
                Feeds found from a site's address alone — paste example.com and
                it goes looking. Where a site offers several you pick the ones
                you want, and you can read a feed's last ten articles before
                deciding to follow it
              </Feature>
              <Feature>
                A page for the news and another for the long reads — as many as
                you keep, each drawing from what you tell it
              </Feature>
              <Feature>A reach per feed, from a day to no limit</Feature>
              <Feature>
                Recently read: what you finished with, kept as long as you
                follow the feed it came from
              </Feature>
              <Feature>
                Pictures measured before they are placed, so a panorama gets the
                width it wants and nothing is cropped to a square it was never
                shot for
              </Feature>
              <Feature>Tags, nested as deep as you like</Feature>
              <Feature>
                Your feeds in and out, as a plain list or as OPML — hand them
                over as a link, save them as a file, or paste either kind
                straight in
              </Feature>
              <Feature>Pages you can publish to the open web</Feature>
              <Feature>Invitations, by link or by email</Feature>
            </ul>
          </Section>
        </Reveal>

        <Reveal>
          <Strip>
            <Shot
              src="/landing/feeds.webp"
              alt="The feeds somebody follows, each with the sections it is filed under and a priority slider."
              title="Every feed, and what it is worth to you"
              description="One slider per feed, and it sets a share of the page rather than a place in a running order. A feed at 90 gets nine times the room of one at 10 and neither is ever silenced — zero means never, and it is the only thing that does. The line underneath says where the feed is filed, how far back it reaches, and whether it is still answering."
            />
            <Shot
              src="/landing/feed.webp"
              alt="One feed's dialog: its name, the sections it is filed under, and how far back a page reaches into it."
              title="Everything about one feed, behind its name"
              description="What to call it, which sections it belongs to, how far back a page may reach into it, and the way to stop following. Nothing is written until you save, so a dialog you close changes nothing — and the name is yours, not the publisher's, if theirs is unbearable."
            />
            <Shot
              src="/landing/pages.webp"
              alt="The front pages somebody keeps, with how often each is composed and how much is on it."
              title="As many front pages as you keep"
              description="One for the news, another for the long reads, a third for comics. Each turns on its own clock — hourly, every six hours, daily or weekly — and holds however much you ask it to. They are separate pages, not filters on one: what you have seen on a page of comics is not something the page of everything has shown you."
            />
            <Shot
              src="/landing/page.webp"
              alt="A page's filter: each tag and feed on a switch with three positions."
              title="What each page draws from"
              description="Every tag and every feed on one switch with three positions: take it, say nothing about it, or keep it off. A feed you have an opinion about overrides whatever its tags said, which is the difference between a filter and a rule with exceptions."
            />
            <Shot
              src="/landing/read.webp"
              alt="Recently read: what has been read, by day."
              title="The only list here"
              description="What you have finished with, by day. It counts nothing and never asks for anything — a list of things already dealt with is the one kind that cannot become a chore. It is also what stops a story coming back a year later as though it were new, so it lasts as long as you follow the feed rather than expiring on a timer."
            />
          </Strip>
        </Reveal>

        <Reveal>
          <Section title="Getting in" voice="humanist">
            <p>
              This is somebody's own instance, and it is invitation-only — there
              is no sign-up, and no default password at any point. If you have
              an account,{" "}
              <button
                type="button"
                onClick={() => setSigningIn(true)}
                className="text-accent underline underline-offset-2"
              >
                sign in
              </button>
              . If you were expecting an invitation, it arrives as a link.
            </p>
            <p>
              To run one of your own, it is a single container and one required
              setting. The whole of it — the server, this page, and the
              screenshots above —{" "}
              <a
                href={PRODUCT.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-accent underline underline-offset-2"
              >
                is on GitHub
              </a>
              , and there is nothing else to buy or sign up for.
            </p>
          </Section>
        </Reveal>

        <Colophon className="mt-20 border-t border-rule pt-6" />
      </main>

      <SignInDialog
        open={signingIn}
        onClose={(signedIn) => {
          setSigningIn(false);
          // A whole-document navigation, and it has to be: this document is the one the
          // server hands somebody *without* a session. With the cookie now set, "/" is the
          // reader — but only the server can swap the shell, so staying here would leave
          // somebody signed in and still looking at the sales pitch.
          if (signedIn) window.location.href = "/";
        }}
      />
    </>
  );
}

/**
 * A run of prose under a heading.
 *
 * `wide` is for the parts that are not prose. A measure exists so the eye can find the start of
 * the next line, which a two-column list of six short phrases does not need — held to it, the
 * list crowds into the left half of the page and leaves the right half empty.
 */
function Section({
  title,
  voice,
  wide = false,
  children,
}: {
  title: string;
  /**
   * The display face this heading is set in, named rather than drawn.
   *
   * The front page draws a voice per card from the article's id, because there the point is
   * that nobody chose. Here somebody did: this document is written, and its headings run
   * through the faces in the order they are written in. A page whose typography reshuffled
   * on reload would be saying the faces are decoration, which is the opposite of the claim
   * the page is making about them.
   */
  voice: Voice;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <section className="mt-20 border-t border-rule pt-10">
      <h2 className={`headline heading-voiced voice-${voice} text-ink`}>
        {title}
      </h2>
      <div
        className={`prose-summary mt-4 flex flex-col gap-4 text-ink-muted ${
          wide ? "" : "max-w-2xl"
        }`}
      >
        {children}
      </div>
    </section>
  );
}

function Difference({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div>
      <h3 className="font-serif text-lg text-ink">{title}</h3>
      <p className="mt-1">{children}</p>
    </div>
  );
}

function Feature({ children }: { children: ReactNode }) {
  return <li className="text-sm text-ink-muted">{children}</li>;
}

/**
 * A screenshot, generated rather than drawn.
 *
 * These come out of the same run that makes the README's — docs/screenshots/capture.mjs takes
 * every shot twice, once at retina for the README and once at half that for here. One run, one
 * set of stand-in publishers, one composed page: the two sets cannot drift, and neither can
 * drift from the product, because both are photographs of it.
 *
 * Half scale because a picture here is scenery beside the words. The retina front page alone
 * is seven hundred kilobytes, on the one document that exists to be read by somebody who has
 * not decided to stay yet; these three together are under four hundred.
 */
function Shot({
  src,
  alt,
  title,
  description,
  className = "",
}: {
  src: string;
  alt: string;
  title?: string;
  description?: string;
  className?: string;
}) {
  const image = (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      decoding="async"
      className={`w-full rounded-md border border-rule ${className}`}
    />
  );
  if (!title) return image;

  return (
    // The caption first, and it carries the weight rather than labelling the picture. Under
    // the image it is a note on something already examined; above it, it is what decides
    // whether the picture is worth examining — which is the job it has in a strip somebody is
    // scrolling past. A screenshot at this size can hold a sentence or two of explanation
    // beside it, so it does.
    <figure className="w-[min(78vw,44rem)] shrink-0">
      <figcaption className="mb-4">
        <h3 className="font-serif text-lg text-ink">{title}</h3>
        {description ? (
          <p className="prose-summary mt-1 text-sm text-ink-muted">
            {description}
          </p>
        ) : null}
      </figcaption>
      {image}
    </figure>
  );
}

/**
 * A row of screenshots that runs off the edge.
 *
 * Six of these stacked is a page nobody reaches the end of, and six shrunk to fit across is six
 * pictures of nothing legible. A strip keeps them at a size where the words in them can be read
 * and lets the page stay short — and running past the edge is itself the invitation to push it.
 *
 * It breaks the container to the window's full width, because a strip that stops at the text
 * measure looks like a mistake rather than a strip: the whole gesture is that there is more
 * than fits.
 */
function Strip({ children }: { children: ReactNode }) {
  return (
    // The padding is on the row, not on the scroll port. A scroll container's trailing
    // padding is not honoured at the end of the scroll — the last card ends flush against the
    // window edge, which reads as the strip having been cut off rather than having ended.
    // Padding the element that scrolls puts the space inside the scrollable width, where the
    // end of the travel can actually reach it.
    <div className="mt-8 -mx-6 overflow-x-auto pb-2">
      {/* `items-start`, so each picture sits directly under the words that introduce it. The
          alternative — every picture starting at the same height, whatever the length of its
          caption — lines the pictures up with each other and leaves a hole under the shorter
          captions, which reads as something missing rather than as alignment. A caption
          belongs to the picture below it, and that bond is worth more than a straight edge. */}
      {/* `w-max`, and it is what makes the padding work. Left to itself the row is a block
          filling the scroller, its cards overflow *it* rather than widening it, and the
          padding-right then sits at the row's own right edge — inside the scroll, never after
          the last card. Sized to its contents, the padding is part of the scrollable width and
          the travel can reach it. */}
      <div className="flex w-max items-start gap-5 px-6">{children}</div>
    </div>
  );
}

/**
 * Fades its children in the first time they come near the window.
 *
 * The reader itself is under a rule that nothing on the page moves, and that rule is about the
 * reader: a newspaper does not react to being looked at. This page is not a newspaper, it is
 * an argument, and a section arriving as you reach it is how an argument is paced.
 *
 * Once, never back — a section that faded out again would be a section arguing with the reader
 * about where they are. And nothing at all under `prefers-reduced-motion`, where the whole page
 * is simply present, which is also what happens without JavaScript or IntersectionObserver.
 */
function Reveal({ children }: { children: ReactNode }) {
  const seen = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    const node = seen.current;
    if (!node || typeof IntersectionObserver === "undefined") {
      setShown(true);
      return;
    }
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
      setShown(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setShown(true);
          observer.disconnect();
        }
      },
      // A little before it arrives, so the fade finishes about when it is being read rather
      // than starting then.
      { rootMargin: "0px 0px -10% 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  return (
    <div
      ref={seen}
      className={`transition-[opacity,translate] duration-700 ease-out motion-reduce:transition-none ${
        shown ? "translate-y-0 opacity-100" : "translate-y-3 opacity-0"
      }`}
    >
      {children}
    </div>
  );
}
