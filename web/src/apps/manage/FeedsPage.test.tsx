import { screen, waitFor, within } from "@testing-library/react";
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
    feed_id: "f_1",
    url: "https://example.com/rss",
    site_url: "https://example.com",
    title: "The Example",
    feed_title: "The Example",
    title_override: "",
    note: "",
    priority: 50,
    tag_ids: [],
    article_window: 604800,
    created_at: now - 3 * 86400,
    last_success_at: now - 600,
    last_status: 0,
    last_error: "",
    last_error_body: "",
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
      "News / World · Art · 1w · added 3 days ago · fetched 10 minutes ago",
    );
  });

  // The label is short enough to be meaningless on its own, so the sentence it replaced
  // has to still be somewhere.
  it("keeps the reach readable for anyone who does not know what 1w means", async () => {
    render([subscription({ article_window: 0 })], [news]);

    const label = await screen.findByText("no limit");
    expect(label).toHaveAttribute("title", "reaches back without limit");

    render([subscription({ article_window: 2592000 })], [news]);
    expect(await screen.findByText("1m")).toHaveAttribute(
      "title",
      "reaches back a month",
    );
  });

  it("says when it was added even with no tags", async () => {
    render([subscription()], [news]);

    expect(await line()).toBe("1w · added 3 days ago · fetched 10 minutes ago");
  });

  /*
   * A name is often not enough to place a feed a year later — "Notes", "Blog", somebody's
   * name — and the site itself answers in one click what no amount of metadata would.
   */
  it("offers the way back to the site a feed comes from", async () => {
    render([subscription({ site_url: "https://example.com/blog/" })], []);

    const link = await screen.findByRole("link", { name: "example.com/blog" });
    // The address it actually goes to is the whole one; the scheme and the bare trailing
    // slash are dropped from what is *shown*, because neither carries anything here.
    expect(link).toHaveAttribute("href", "https://example.com/blog/");
    expect(link).toHaveAttribute("target", "_blank");
  });

  // Nor does a leading www, which is three characters of every address saying nothing about
  // which address it is.
  it("says an address the way somebody would", async () => {
    render([subscription({ site_url: "https://www.example.com" })], []);

    const link = await screen.findByRole("link", { name: "example.com" });
    expect(link).toHaveAttribute("href", "https://www.example.com");
  });

  // A publisher that names no site leaves nothing to link to, and a dead link would be
  // worse than none.
  it("says nothing about the site when the feed names none", async () => {
    render([subscription({ site_url: "" })], []);

    await screen.findByRole("button", { name: "The Example" });
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  /*
   * Why a feed is here is the one thing about it nothing else can say. The name is the
   * publisher's and the tags are a filing system; this is what somebody wrote down.
   */
  it("shows the note somebody wrote about a feed", async () => {
    render(
      [subscription({ note: "The only place that covers the harbour works." })],
      [],
    );

    expect(
      await screen.findByText("The only place that covers the harbour works."),
    ).toBeInTheDocument();
  });

  // Empty on almost every feed, and a list of forty showing thirty-eight blanks would be
  // worse than one showing two notes.
  it("leaves out the note when there is none", async () => {
    render([subscription({ note: "" })], []);

    const name = await screen.findByRole("button", { name: "The Example" });
    expect(name.closest("div")?.parentElement?.textContent).not.toContain(
      "Why you read it",
    );
  });

  it("writes a note from the dialog", async () => {
    const { transport } = render([subscription()], []);

    await userEvent.click(
      await screen.findByRole("button", { name: "The Example" }),
    );
    await userEvent.type(
      screen.getByLabelText("Why you read it"),
      "Kept for the obituaries.",
    );
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.filter((call) => call.method === "PATCH");
      expect(sent).toHaveLength(1);
      expect((sent[0]?.body as { note: string }).note).toBe(
        "Kept for the obituaries.",
      );
    });
  });

  /*
   * The same dialog the picker uses before subscribing, because "is this still worth
   * having" is "was this worth taking" asked later and deserves the same answer.
   */
  it("previews a feed it already follows, with nothing to add", async () => {
    renderWith(<FeedsPage />, {
      "GET /api/feeds": { body: [subscription()] },
      "GET /api/tags": { body: [] },
      "POST /api/feeds/preview": {
        body: {
          title: "The Example",
          site_url: "https://example.com",
          feed_url: "https://example.com/rss",
          items: [
            {
              title: "A story about a thing",
              link: "https://example.com/1",
              summary: "<p>What the thing was.</p>",
              published_at: now - 3600,
            },
          ],
        },
      },
    });

    await userEvent.click(
      await screen.findByRole("button", { name: "Preview The Example" }),
    );

    const shown = await screen.findByText("A story about a thing");
    const dialog = within(shown.closest("dialog")!);
    // Nothing to say yes to: this feed is already followed, so the dialog is read and
    // closed rather than answered.
    expect(dialog.getByRole("button", { name: "Close" })).toBeInTheDocument();
    expect(
      dialog.queryByRole("button", { name: "Add" }),
    ).not.toBeInTheDocument();
  });

  // Everything about a feed now lives behind its name, so the summary line is not
  // something that appears and disappears — it is simply always there.
  it("opens everything about a feed from its name", async () => {
    render([subscription({ tag_ids: ["t_art"] })], [art]);

    expect(await line()).toBe(
      "Art · 1w · added 3 days ago · fetched 10 minutes ago",
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
      "Art · 1w · added 3 days ago · fetched 10 minutes ago",
    );
  });

  // Some publishers title their feed "technology archives | designboom | architecture &
  // design magazine". The name in a list is the subscriber's to choose.
  it("renames a feed", async () => {
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
    await userEvent.type(field, "Design Boom");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toMatchObject({ title_override: "Design Boom" });
    });
  });

  // A feed with no name at all is not something anybody means to ask for.
  it("refuses to save a feed with no name", async () => {
    render([subscription()], []);

    await userEvent.click(
      await screen.findByRole("button", { name: "The Example" }),
    );
    await userEvent.clear(screen.getByLabelText("What to call it"));

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  // The way back to the publisher's title, without having to know it or retype it.
  it("puts the publisher's title back on request", async () => {
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
    await userEvent.click(
      screen.getByRole("button", { name: "Use publisher title" }),
    );

    const field = screen.getByLabelText("What to call it");
    expect(field).toHaveValue("Theirs");
    // It fills the field rather than saving, so it is a suggestion until Save is pressed.
    expect(
      transport.calls.filter((call) => call.method === "PATCH"),
    ).toHaveLength(0);
    // And it goes, because the field already says that.
    expect(
      screen.queryByRole("button", { name: "Use publisher title" }),
    ).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      // Their title stored as no override, rather than as an override saying the same
      // thing as the title it overrides.
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
        note: "",
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

/*
 * Looking at a feed before following it.
 *
 * A feed's title and address say almost nothing about it — a site offering "Posts",
 * "Comments" and "Notes" is three plausible names and one right answer — and finding out used
 * to mean following one and then unfollowing it again, losing the read marks with it.
 */
describe("FeedsPage, before following anything", () => {
  const candidate = (title: string, url: string) => ({
    title,
    feed_url: url,
    site_url: "https://example.com",
    priority: 50,
    reach: 604800,
    tags: [],
    already_subscribed: false,
  });

  const preview = {
    title: "The Example",
    site_url: "https://example.com",
    feed_url: "https://example.com/rss",
    items: [
      {
        title: "A story about a thing",
        link: "https://example.com/1",
        summary: "<p>What the thing was.</p>",
        published_at: now - 3600,
      },
    ],
  };

  const type = async (address: string) => {
    await userEvent.type(
      await screen.findByLabelText("Feed or site address"),
      address,
    );
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
  };

  it("shows one discovered feed rather than subscribing to it", async () => {
    const { transport } = renderWith(<FeedsPage />, {
      "GET /api/feeds": { body: [] },
      "GET /api/tags": { body: [] },
      "POST /api/feeds/discover": {
        body: { candidates: [candidate("The Example", preview.feed_url)] },
      },
      "POST /api/feeds/preview": { body: preview },
    });

    await type("example.com");

    expect(
      await screen.findByText("A story about a thing"),
    ).toBeInTheDocument();
    expect(screen.getByText("What the thing was.")).toBeInTheDocument();

    // And nothing was taken. This is the moment somebody can still say no cheaply.
    expect(
      transport.calls.some((call) => call.path === "/api/feeds/import"),
    ).toBe(false);
  });

  it("subscribes when the preview is accepted", async () => {
    const { transport } = renderWith(<FeedsPage />, {
      "GET /api/feeds": { body: [] },
      "GET /api/tags": { body: [] },
      "POST /api/feeds/discover": {
        body: { candidates: [candidate("The Example", preview.feed_url)] },
      },
      "POST /api/feeds/preview": { body: preview },
      "POST /api/feeds/import": { body: { added: 1, skipped: [], tags: [] } },
    });

    await type("example.com");
    // Scoped to the dialog: the form behind it has an Add of its own, and this test is
    // about the one somebody presses after reading.
    const shown = await screen.findByText("A story about a thing");
    await userEvent.click(
      within(shown.closest("dialog")!).getByRole("button", { name: "Add" }),
    );

    await waitFor(() =>
      expect(
        transport.calls.some((call) => call.path === "/api/feeds/import"),
      ).toBe(true),
    );
  });

  /*
   * A site that turns out to offer five feeds chose none of them, so the list starts with
   * nothing ticked — otherwise "None" is the first thing anybody has to press.
   */
  it("starts a list of several with nothing chosen", async () => {
    renderWith(<FeedsPage />, {
      "GET /api/feeds": { body: [] },
      "GET /api/tags": { body: [] },
      "POST /api/feeds/discover": {
        body: {
          candidates: [
            candidate("Posts", "https://example.com/posts.xml"),
            candidate("Comments", "https://example.com/comments.xml"),
          ],
        },
      },
    });

    await type("example.com");

    await screen.findByText("Posts");
    for (const box of screen.getAllByRole("checkbox")) {
      expect(box).not.toBeChecked();
    }
    expect(screen.getByText("0 of 2")).toBeInTheDocument();
  });

  it("ticks the row it was opened from, and leaves the list up", async () => {
    const { transport } = renderWith(<FeedsPage />, {
      "GET /api/feeds": { body: [] },
      "GET /api/tags": { body: [] },
      "POST /api/feeds/discover": {
        body: {
          candidates: [
            candidate("Posts", "https://example.com/posts.xml"),
            candidate("Comments", "https://example.com/comments.xml"),
          ],
        },
      },
      "POST /api/feeds/preview": { body: preview },
    });

    await type("example.com");
    await screen.findByText("Posts");

    await userEvent.click(
      screen.getAllByRole("button", { name: "Preview" })[0]!,
    );
    // Scoped to the preview: the form and the picker behind it both have an Add.
    const shown = await screen.findByText("A story about a thing");
    await userEvent.click(
      within(shown.closest("dialog")!).getByRole("button", { name: "Add" }),
    );

    // One ticked, and the picker still open with the other still to decide.
    expect(await screen.findByText("1 of 2")).toBeInTheDocument();
    expect(screen.getAllByRole("checkbox")[0]).toBeChecked();
    expect(screen.getAllByRole("checkbox")[1]).not.toBeChecked();
    // Ticking is not importing: the list is finished at the bottom, not here.
    expect(
      transport.calls.some((call) => call.path === "/api/feeds/import"),
    ).toBe(false);
  });
});
