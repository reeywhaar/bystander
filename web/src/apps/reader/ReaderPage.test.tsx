import { screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Article, Edition, Me, Page } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { ReaderPage } from "@app/apps/reader/ReaderPage";

const me: Me = {
  id: "p_1",
  username: "alice",
  role: "user",
  created_at: 1_787_000_000,
};

function article(id: string, overrides: Partial<Article> = {}): Article {
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
    ...overrides,
  };
}

function edition(items: Article[]): Edition {
  return {
    id: "e_1",
    generated_at: 1_787_000_000,
    next_edition_at: Math.floor(Date.now() / 1000) + 7200,
    size: 60,
    items,
  };
}

/** A front page, as the tab strip sees it. */
function page(overrides: Partial<Page> = {}): Page {
  return {
    id: "pg_1",
    name: "Front Page",
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
    publish_slug: "",
    published: false,
    indexable: false,
    ...overrides,
  };
}

/** The reader as it is actually mounted: two routes onto one component. */
function reader() {
  return (
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route path="/" element={<ReaderPage me={me} />} />
        <Route path="/f/:slug" element={<ReaderPage me={me} />} />
      </Routes>
    </MemoryRouter>
  );
}

describe("ReaderPage composing", () => {
  let scrolled: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    // jsdom has a window.scrollTo that refuses to do anything and complains; this is also
    // the only way to see that it was asked.
    scrolled = vi.fn();
    vi.stubGlobal("scrollTo", scrolled);
  });

  const refusal = {
    "GET /api/pages": {
      body: [
        page(),
        page({ id: "pg_2", name: "Art", slug: "art", is_main: false }),
      ],
    },
    "GET /api/edition": { body: edition([article("a_1")]) },
    "GET /api/feeds": { body: [{ id: "s_1", title: "The Example" }] },
    "POST /api/edition/regenerate": {
      status: 409,
      body: {
        error:
          "everything here has been read, and nothing new has been published yet",
      },
    },
  };

  // The button is at the bottom of the page and everything it has to say is at the top. It
  // scrolled only on success, so a refusal looked like the button doing nothing at all.
  it("goes back to the top whether or not it composed anything", async () => {
    renderWith(reader(), refusal);
    const compose = await screen.findByRole("button", {
      name: "Make a different page",
    });

    // Arriving at the page scrolls to the top as well, so only what happens after the
    // press says anything about the press.
    scrolled.mockClear();
    await userEvent.click(compose);

    expect(
      await screen.findByText(/everything here has been read/),
    ).toBeInTheDocument();
    expect(scrolled).toHaveBeenCalledWith({ top: 0 });
  });

  // Both routes render this component, so React hands it new props rather than remounting —
  // and the refusal sat there over a page it was never said of.
  it("leaves the last page's refusal behind when you move to another tab", async () => {
    renderWith(reader(), refusal);

    await userEvent.click(
      await screen.findByRole("button", { name: "Make a different page" }),
    );
    await screen.findByText(/everything here has been read/);

    scrolled.mockClear();
    await userEvent.click(screen.getByRole("link", { name: "Art" }));

    await waitFor(() => {
      expect(screen.queryByText(/everything here has been read/)).toBeNull();
    });
    // And arriving at a different page means arriving at the top of it.
    expect(scrolled).toHaveBeenCalledWith({ top: 0 });
  });
});

describe("ReaderPage", () => {
  it("lays out the page it was given", async () => {
    renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": {
          body: edition([
            article("a_1", { slot: "lead", title: "The lead" }),
            article("a_2", { rank: 1 }),
          ]),
        },
        "GET /api/feeds": { body: [] },
      },
    );

    expect(
      await screen.findByRole("link", { name: "The lead" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Story a_2" })).toBeInTheDocument();
  });

  // A new account should be told what to do, not shown a broken page.
  it("sends somebody with no feeds to add one", async () => {
    renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": { body: edition([]) },
        "GET /api/feeds": { body: [] },
      },
    );

    expect(
      await screen.findByText("Nothing on the page yet"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Add your first feed" }),
    ).toHaveAttribute("href", "/manage");
  });

  // Feeds added but no page yet is the ordinary state for the first minute or two of a
  // fresh instance, and the thing somebody wants there is to stop waiting.
  it("offers to compose the first page once there are feeds", async () => {
    const { transport } = renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": { body: edition([]) },
        "GET /api/feeds": { body: [{ id: "s_1", title: "The Example" }] },
        "POST /api/edition/regenerate": { body: edition([article("a_1")]) },
      },
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "Make my first page" }),
    );

    expect(
      await screen.findByRole("link", { name: "Story a_1" }),
    ).toBeInTheDocument();
    expect(transport.calls).toContainEqual(
      expect.objectContaining({
        method: "POST",
        path: "/api/edition/regenerate",
      }),
    );
  });

  it("marks an article read without waiting for the server", async () => {
    const { transport } = renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": { body: edition([article("a_1")]) },
        "PUT /api/edition/items/a_1/read": { status: 204 },
      },
    );

    await screen.findByRole("link", { name: "Story a_1" });
    await userEvent.click(screen.getByRole("button", { name: "Mark read" }));

    // The card greys from the optimistic write, and the request follows it.
    expect(
      await screen.findByRole("button", { name: "Mark unread" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(transport.calls).toContainEqual(
        expect.objectContaining({
          method: "PUT",
          path: "/api/edition/items/a_1/read",
        }),
      ),
    );
  });

  // Composing no longer refuses when everything has been read — it shuffles instead — but a
  // refusal from anywhere else is still a note about the world rather than a fault, and the
  // page it declined to replace has to still be there.
  it("keeps the page when composing is refused", async () => {
    renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": { body: edition([article("a_1")]) },
        "POST /api/edition/regenerate": {
          status: 409,
          body: {
            error: "nothing new has been published since this page was made",
          },
        },
      },
    );

    await screen.findByRole("link", { name: "Story a_1" });
    await userEvent.click(
      screen.getByRole("button", { name: "Make a different page" }),
    );

    expect(
      await screen.findByText(
        "nothing new has been published since this page was made",
      ),
    ).toBeInTheDocument();
    // A refusal must not scroll: nothing was replaced.
    expect(window.scrollY).toBe(0);
    expect(screen.getByRole("link", { name: "Story a_1" })).toBeInTheDocument();
  });

  // Composing used to refuse when everything had been read, which left somebody who had
  // worked through their page with nothing to press at all. It shuffles now, and says so —
  // otherwise the button appears to have done nothing.
  it("says so when the new page is entirely things already read", async () => {
    const read = { ...article("a_1"), read_at: 1756000000 };
    renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": { body: edition([article("a_1")]) },
        "POST /api/edition/regenerate": { body: edition([read]) },
      },
    );

    await screen.findByRole("link", { name: "Story a_1" });
    await userEvent.click(
      screen.getByRole("button", { name: "Make a different page" }),
    );

    expect(
      await screen.findByText(/You have read everything here/),
    ).toBeInTheDocument();
  });

  // And not when there is anything unread on it, or the note would be on every page.
  it("says nothing when the new page has something unread on it", async () => {
    renderWith(
      <MemoryRouter>
        <ReaderPage me={me} />
      </MemoryRouter>,
      {
        "GET /api/edition": { body: edition([article("a_1")]) },
        "POST /api/edition/regenerate": {
          body: edition([
            { ...article("a_1"), read_at: 1756000000 },
            article("a_2"),
          ]),
        },
      },
    );

    await screen.findByRole("link", { name: "Story a_1" });
    await userEvent.click(
      screen.getByRole("button", { name: "Make a different page" }),
    );
    await screen.findByRole("link", { name: "Story a_2" });

    expect(
      screen.queryByText(/You have read everything here/),
    ).not.toBeInTheDocument();
  });
});
