import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Article, Edition, Me, PublicPage } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { App } from "@app/apps/public/App";
import { ReaderPage } from "@app/apps/reader/ReaderPage";

const EDITION = "e_06G39S3WPC4060DVJ1VA4X2EXM";

const me: Me = {
  id: "p_1",
  username: "misha",
  role: "user",
  created_at: 1_787_000_000,
};

function article(id: string): Article {
  return {
    id,
    rank: 0,
    slot: "standard",
    read_at: null,
    title: `Story ${id}`,
    link: `https://example.com/${id}`,
    author: "",
    summary: "<p>A standfirst</p>",
    image_url: "",
    image_width: 0,
    image_height: 0,
    published_at: 1_787_000_000,
    feed: {
      id: "f_1",
      title: "The Example",
      site_url: "https://example.com",
      subscription_id: "s_1",
      priority: 50,
    },
  };
}

// Enough of them that a wrong seed cannot agree by luck. Each card draws a voice, a width, a
// frame and a prose step, so two independent draws matching on one card is unremarkable and
// matching on twelve is not.
const ITEMS = Array.from({ length: 12 }, (_, i) => article(`a_${i}`));

/*
 * The same articles, as a published page really carries them.
 *
 * A published page's feed stubs are built from the *owner's* subscriptions, so the server
 * sends the name and the address and neither the subscription nor the priority — there is
 * nothing on somebody else's page for a visitor to act on, and an id belonging to another
 * account is the one thing that is not theirs to have. See publish.go.
 *
 * Nothing that decides how a card *looks* differs, which is what keeps the seeding test below
 * comparing like with like.
 */
const PUBLISHED_ITEMS = ITEMS.map((item) => ({
  ...item,
  feed: { ...item.feed, subscription_id: "", priority: 0 },
}));

const published: PublicPage = {
  id: EDITION,
  name: "Comics",
  generated_at: 1_787_000_000,
  signed_in: false,
  indexable: false,
  items: PUBLISHED_ITEMS,
};

const edition: Edition = {
  id: EDITION,
  generated_at: 1_787_000_000,
  next_edition_at: 1_787_100_000,
  size: 60,
  items: ITEMS,
};

/** How every card looks, in order — which is all the seed decides. */
function looks() {
  return [...document.querySelectorAll("article")].map(
    (card) => card.className,
  );
}

describe("a published page", () => {
  // The one thing that makes a published page the *same* page rather than another page with
  // the same articles on it. Every card's appearance is drawn from the edition's id and the
  // article's, so seeding the two screens differently produces two pages that agree on every
  // word and nothing else — different faces, different widths, different boxes.
  //
  // This was wrong: the published page seeded on the composition time instead, which is
  // stable — a visitor saw the same thing on every reload — and completely unlike what the
  // owner saw. Stability was never the property that was wanted.
  it("draws its cards exactly as the owner's own page does", async () => {
    const { unmount } = renderWith(<ReaderPage me={me} />, {
      "GET /api/pages": {
        body: [
          {
            id: "pg_1",
            name: "Comics",
            slug: "",
            is_main: true,
            edition_interval: 86400,
            edition_size: 60,
            next_edition_at: 1_787_000_000,
            max_article_age: 0,
            include_tag_ids: [],
            exclude_tag_ids: [],
            include_feed_ids: [],
            exclude_feed_ids: [],
            publish_slug: "comics",
            published: true,
            indexable: false,
          },
        ],
      },
      "GET /api/edition": { body: edition },
      "GET /api/feeds": { body: [{ id: "s_1", title: "The Example" }] },
    });

    await waitFor(() => expect(looks()).toHaveLength(ITEMS.length));
    const owner = looks();
    unmount();

    window.history.pushState({}, "", "/p/misha/comics");
    renderWith(<App />, {
      "GET /api/public/misha/comics": { body: published },
      "GET /api/me": { status: 401, body: { error: "no session" } },
    });

    await screen.findByRole("heading", { name: "Comics" });
    await waitFor(() => expect(looks()).toHaveLength(ITEMS.length));

    expect(looks()).toEqual(owner);
  });

  /*
   * Acting on a feed is acting on somebody's subscription, and on a published page every
   * subscription belongs to the owner.
   *
   * Two things keep it off this page, and both are deliberate. The island passes no
   * `onActions`; and the server sends no `subscription_id` on a published page's stubs, so
   * the card would refuse to draw the control even if it were handed one — see publish.go,
   * which builds those stubs from the owner's own subscriptions and must not hand their ids
   * to whoever opens a link.
   */
  it("offers a visitor no way to act on the owner's feeds", async () => {
    window.history.pushState({}, "", "/p/misha/comics");
    renderWith(<App />, {
      // Signed in, so the visitor gets Mark read — which records against *them*. That is the
      // one thing a published page does offer an account, and it is not this.
      "GET /api/public/misha/comics": {
        body: { ...published, signed_in: true },
      },
      "GET /api/me": { body: me },
      "PUT /api/edition/items/a_0/read": { status: 204 },
    });

    await screen.findByRole("heading", { name: "Comics" });
    await waitFor(() => expect(looks()).toHaveLength(ITEMS.length));

    expect(
      screen.getAllByRole("button", { name: "Mark read" }),
    ).not.toHaveLength(0);
    expect(screen.queryByRole("button", { name: /More about/ })).toBeNull();
  });
});
