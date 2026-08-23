import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { PlannedFeed, Tag } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { ImportDialog } from "@app/apps/manage/ImportDialog";

function planned(overrides: Partial<PlannedFeed> = {}): PlannedFeed {
  return {
    title: "A feed",
    feed_url: "https://example.com/rss",
    site_url: "https://example.com",
    priority: 50,
    already_subscribed: false,
    tags: [],
    ...overrides,
  };
}

const art: Tag = {
  id: "t_art",
  name: "Art",
  parent_id: null,
  priority: 50,
  created_at: 0,
};

async function paste(feeds: PlannedFeed[], tags: Tag[] = []) {
  const rendered = renderWith(<ImportDialog open onClose={() => {}} />, {
    "GET /api/tags": { body: tags },
    "POST /api/feeds/import/preview": { body: { feeds } },
    "POST /api/feeds/import": {
      body: { added: feeds.length, skipped: 0, failed: [], tags_created: [] },
    },
  });

  await userEvent.type(screen.getByLabelText("The list to import"), "anything");
  await userEvent.click(screen.getByRole("button", { name: "Read it" }));
  return rendered;
}

describe("ImportDialog", () => {
  // Ticking a feed you already follow does nothing — the server refuses a second
  // subscription — so it is not offered.
  it("leaves out feeds you already follow, and says how many", async () => {
    await paste([
      planned({ title: "New one", feed_url: "https://new.example/rss" }),
      planned({
        title: "Old one",
        feed_url: "https://old.example/rss",
        already_subscribed: true,
      }),
    ]);

    expect(await screen.findByText("New one")).toBeInTheDocument();
    expect(screen.queryByText("Old one")).not.toBeInTheDocument();

    // Counted even though it is not shown, so an overlapping list does not just look short.
    expect(
      screen.getByText(/1 is already yours, and not shown/),
    ).toBeInTheDocument();
    expect(screen.getByText("1 of 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add 1" })).toBeInTheDocument();
  });

  it("says so when the whole list is already yours", async () => {
    await paste([planned({ already_subscribed: true })]);

    expect(
      await screen.findByText("You already follow everything in that list."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add 0" })).toBeDisabled();
  });

  // Tags the list named that you have are a match, not a decision. Tags you do not have
  // should arrive because somebody asked, not because they came in the post.
  it("ticks the tags you have and leaves the new ones off", async () => {
    await paste(
      [
        planned({
          tags: [
            { path: ["Art"], name: "Art", tag_id: "t_art" },
            { path: ["Woodworking"], name: "Woodworking", tag_id: "" },
          ],
        }),
      ],
      [art],
    );

    const mine = await screen.findByRole("button", { name: "Art" });
    expect(mine).toHaveAttribute("aria-pressed", "true");

    const incoming = screen.getByRole("button", { name: "Woodworking +" });
    expect(incoming).toHaveAttribute("aria-pressed", "false");
  });

  // Every tag you own is offered under every feed, not just the ones the list mentioned —
  // filing a stranger's feed is the moment you know where it belongs.
  it("offers every tag you own, mentioned or not", async () => {
    await paste([planned({ tags: [] })], [art]);

    const chip = await screen.findByRole("button", { name: "Art" });
    expect(chip).toHaveAttribute("aria-pressed", "false");
  });

  it("sends only what was ticked", async () => {
    const { transport } = await paste(
      [
        planned({
          tags: [{ path: ["Woodworking"], name: "Woodworking", tag_id: "" }],
        }),
      ],
      [art],
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "Woodworking +" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Add 1" }));

    await waitFor(() => {
      const sent = transport.calls.find(
        (call) => call.path === "/api/feeds/import",
      );
      expect(sent).toBeDefined();
      const body = sent?.body as { feeds: { tag_paths: string[][] }[] };
      expect(body.feeds[0]?.tag_paths).toEqual([["Woodworking"]]);
    });
  });
});
