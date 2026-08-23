import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Subscription, Tag } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { FeedsPage } from "@app/apps/manage/FeedsPage";

const now = Math.floor(Date.now() / 1000);

const news: Tag = {
  id: "t_news",
  name: "News",
  parent_id: null,
  priority: 50,
  created_at: 0,
};
const world: Tag = {
  id: "t_world",
  name: "World",
  parent_id: "t_news",
  priority: 50,
  created_at: 0,
};
const art: Tag = {
  id: "t_art",
  name: "Art",
  parent_id: null,
  priority: 50,
  created_at: 0,
};

function subscription(overrides: Partial<Subscription> = {}): Subscription {
  return {
    id: "s_1",
    url: "https://example.com/rss",
    site_url: "https://example.com",
    title: "The Example",
    feed_title: "The Example",
    title_override: "",
    priority: 50,
    tag_ids: [],
    article_window: 604800,
    created_at: now - 3 * 86400,
    last_success_at: now - 600,
    last_error: "",
    failure_count: 0,
    ...overrides,
  };
}

function render(feeds: Subscription[], tags: Tag[]) {
  return renderWith(<FeedsPage />, {
    "GET /api/feeds": { body: feeds },
    "GET /api/tags": { body: tags },
  });
}

describe("FeedsPage", () => {
  // The second line carries what the first has no room for. Matched on the line rather
  // than on a fragment of it, because the tags sit in their own span inside it.
  const line = async () =>
    (await screen.findByText(/added 3 days ago/)).textContent ?? "";

  it("names a feed's tags and how long it has been there", async () => {
    render(
      [subscription({ tag_ids: ["t_world", "t_art"] })],
      [news, world, art],
    );

    // Full paths: a nested tag reads as where it sits, not just its own name.
    expect(await line()).toBe(
      "News / World · Art · added 3 days ago · fetched 10 minutes ago",
    );
  });

  it("says when it was added even with no tags", async () => {
    render([subscription()], [news]);

    expect(await line()).toBe("added 3 days ago · fetched 10 minutes ago");
  });

  // Everything about a feed now lives behind its name, so the summary line is not
  // something that appears and disappears — it is simply always there.
  it("opens everything about a feed from its name", async () => {
    render([subscription({ tag_ids: ["t_art"] })], [art]);

    expect(await line()).toBe(
      "Art · added 3 days ago · fetched 10 minutes ago",
    );

    await userEvent.click(screen.getByRole("button", { name: "The Example" }));

    // The name, the filing and the reach, all in one place.
    expect(screen.getByLabelText("What to call it")).toHaveValue("The Example");
    expect(
      screen.getByRole("button", { name: "Art", pressed: true }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "A week", pressed: true }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Stop following" }),
    ).toBeInTheDocument();

    // …and the summary is still under the row behind it.
    expect(await line()).toBe(
      "Art · added 3 days ago · fetched 10 minutes ago",
    );
  });

  // Some publishers title their feed "technology archives | designboom | architecture &
  // design magazine". The name in a list is the subscriber's to choose.
  it("renames a feed without losing the publisher's own name", async () => {
    const { transport } = render(
      [subscription({ title: "unbearable | name | here" })],
      [],
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "unbearable | name | here" }),
    );

    const field = screen.getByLabelText("What to call it");
    expect(field).toHaveValue("unbearable | name | here");
    expect(screen.getByText(/The publisher calls it/)).toBeInTheDocument();

    await userEvent.clear(field);
    await userEvent.type(field, "Design Boom");
    await userEvent.click(screen.getByRole("button", { name: "Rename" }));

    const sent = transport.calls.find((call) => call.method === "PATCH");
    expect(sent?.body).toEqual({ title_override: "Design Boom" });
  });

  // Clearing the field is how you go back to the publisher's name, and storing an override
  // that says the same thing as theirs would be storing nothing.
  it("stores no override when the name is cleared", async () => {
    const { transport } = render(
      [
        subscription({
          title: "Mine",
          feed_title: "Theirs",
          title_override: "Mine",
        }),
      ],
      [],
    );

    await userEvent.click(await screen.findByRole("button", { name: "Mine" }));

    await userEvent.clear(screen.getByLabelText("What to call it"));
    await userEvent.click(screen.getByRole("button", { name: "Rename" }));

    const sent = transport.calls.find((call) => call.method === "PATCH");
    expect(sent?.body).toEqual({ title_override: "" });
  });

  // The reach is the feed's, not the reader's.
  it("sets how far back a page reaches into one feed", async () => {
    const { transport } = render([subscription()], []);

    await userEvent.click(
      await screen.findByRole("button", { name: "The Example" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "A month" }));

    const sent = transport.calls.find((call) => call.method === "PATCH");
    expect(sent?.body).toEqual({ article_window: 2592000 });
  });
});
