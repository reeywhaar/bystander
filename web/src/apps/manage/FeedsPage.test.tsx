import { screen, waitFor } from "@testing-library/react";
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
    // Without this the write fails, `onSuccess` never runs, and nothing is invalidated —
    // which would quietly hide the very thing these tests are about.
    "PATCH /api/feeds/s_1": { status: 204 },
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
  it("renames a feed, showing whose name an empty field falls back to", async () => {
    const { transport } = render(
      [subscription({ title: "unbearable | name | here" })],
      [],
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "unbearable | name | here" }),
    );

    const field = screen.getByLabelText("What to call it");
    expect(field).toHaveValue("unbearable | name | here");

    await userEvent.clear(field);
    // Emptied, the field shows what it would fall back to rather than nothing at all.
    expect(field).toHaveAttribute("placeholder", "The Example");

    await userEvent.type(field, "Design Boom");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toMatchObject({ title_override: "Design Boom" });
    });
  });

  // Clearing it is how you go back to the publisher's name, and an override that says the
  // same thing as theirs is no override at all.
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
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toMatchObject({ title_override: "" });
    });
  });

  // Typing the publisher's own name out is the same as leaving it empty.
  it("stores no override when the name matches the publisher's", async () => {
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

    const field = screen.getByLabelText("What to call it");
    await userEvent.clear(field);
    await userEvent.type(field, "Theirs");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toMatchObject({ title_override: "" });
    });
  });

  // Nothing is written until Save, so a toggle has to show immediately from local state
  // rather than waiting on a round trip.
  it("shows a change at once and writes it on save", async () => {
    const { transport } = render([subscription()], [art]);

    await userEvent.click(
      await screen.findByRole("button", { name: "The Example" }),
    );

    await userEvent.click(screen.getByRole("button", { name: "A month" }));
    await userEvent.click(screen.getByRole("button", { name: "Art" }));

    expect(screen.getByRole("button", { name: "A month" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "A week" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(screen.getByRole("button", { name: "Art" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    // Nothing has been written yet.
    expect(
      transport.calls.filter((call) => call.method === "PATCH"),
    ).toHaveLength(0);

    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.filter((call) => call.method === "PATCH");
      expect(sent).toHaveLength(1);
      // One request carrying the whole of what the dialog holds, name included.
      expect(sent[0]?.body).toEqual({
        title_override: "",
        tag_ids: ["t_art"],
        article_window: 2592000,
      });
    });
  });

  // Closing any other way leaves the feed as it was — otherwise there is no way out of a
  // dialog without consequences.
  it("writes nothing when it is cancelled", async () => {
    const { transport } = render([subscription()], [art]);

    await userEvent.click(
      await screen.findByRole("button", { name: "The Example" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "A month" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(
      transport.calls.filter((call) => call.method === "PATCH"),
    ).toHaveLength(0);
  });

  // Saving with nothing changed is a close, not a write.
  it("writes nothing when nothing changed", async () => {
    const { transport } = render([subscription()], [art]);

    await userEvent.click(
      await screen.findByRole("button", { name: "The Example" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(
      transport.calls.filter((call) => call.method === "PATCH"),
    ).toHaveLength(0);
  });
});
