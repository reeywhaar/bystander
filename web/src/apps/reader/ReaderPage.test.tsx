import { screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Article, Edition, Me } from "@app/api/types";
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
    feed: { id: "f_1", title: "The Example", site_url: "https://example.com" },
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

  // A refusal that says "nothing new" is a note about the world, not a fault, and the page
  // it declined to replace has to still be there.
  it("keeps the page when there is nothing new to replace it with", async () => {
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
});
