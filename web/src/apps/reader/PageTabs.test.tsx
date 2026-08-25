import { screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";

import type { Page } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { PageTabs } from "@app/apps/reader/PageTabs";

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

const art = page({ id: "pg_2", name: "Art", slug: "art", is_main: false });

describe("PageTabs", () => {
  // Somebody who has never made a second page should not have to look at a control for
  // choosing between one thing.
  it("shows nothing at all when there is only one page", async () => {
    const { container } = renderWith(
      <MemoryRouter>
        <PageTabs />
      </MemoryRouter>,
      { "GET /api/pages": { body: [page()] } },
    );

    await waitFor(() => {
      expect(container.querySelector("nav")).toBeNull();
    });
  });

  it("links each page to where it is read", async () => {
    renderWith(
      <MemoryRouter>
        <PageTabs />
      </MemoryRouter>,
      { "GET /api/pages": { body: [page(), art] } },
    );

    const main = await screen.findByRole("link", { name: "Front Page" });
    const second = screen.getByRole("link", { name: "Art" });

    // The main page is at the root; the rest carry their slug.
    expect(main.getAttribute("href")).toBe("/");
    expect(second.getAttribute("href")).toBe("/f/art");
  });

  // Without `end` on the main page, "/" counts as active on every route beneath it and the
  // strip lights two tabs at once.
  it("marks only the page being read", async () => {
    renderWith(
      <MemoryRouter initialEntries={["/f/art"]}>
        <PageTabs />
      </MemoryRouter>,
      { "GET /api/pages": { body: [page(), art] } },
    );

    const second = await screen.findByRole("link", { name: "Art" });
    const main = screen.getByRole("link", { name: "Front Page" });

    expect(second.className).toContain("border-accent");
    expect(main.className).not.toContain("border-accent");
  });
});
