import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Tag } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { TagsPage } from "@app/apps/manage/TagsPage";

const tech: Tag = {
  id: "t_tech",
  name: "Tech",
  parent_id: null,
  priority: 50,
  created_at: 0,
};
const nested: Tag = {
  id: "t_ai",
  name: "AI & GPT",
  parent_id: "t_tech",
  priority: 50,
  created_at: 0,
};

const news: Tag = {
  id: "t_news",
  name: "News",
  parent_id: null,
  priority: 50,
  created_at: 0,
};

describe("TagsPage", () => {
  it("makes a tag through the dialog and shows it in the list", async () => {
    const made: Tag = {
      id: "t_art",
      name: "Art",
      parent_id: null,
      priority: 50,
      created_at: 0,
    };
    const { transport } = renderWith(<TagsPage />, {
      "GET /api/tags": { body: [news] },
      "POST /api/tags": { status: 201, body: made },
    });

    await userEvent.click(
      await screen.findByRole("button", { name: "New tag" }),
    );
    await userEvent.type(screen.getByLabelText("Name"), "Art");

    // The list refetches after the write, so what comes back is what it shows.
    transport.recording["GET /api/tags"] = { body: [news, made] };
    await userEvent.click(screen.getByRole("button", { name: "Make it" }));

    expect(await screen.findByLabelText("Name of Art")).toBeInTheDocument();
  });

  /*
   * The field this replaced could only take a name, so every tag arrived on its own at the
   * default weight and the other two decisions were made afterwards, from a row somebody
   * had to find again.
   */
  it("asks all three things before anything is written", async () => {
    const { transport } = renderWith(<TagsPage />, {
      "GET /api/tags": { body: [news] },
    });

    await userEvent.click(
      await screen.findByRole("button", { name: "New tag" }),
    );

    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Where it sits")).toBeInTheDocument();
    expect(screen.getByLabelText("How often it appears")).toBeInTheDocument();

    await waitFor(() => {
      expect(transport.calls.some((call) => call.method === "POST")).toBe(
        false,
      );
    });
  });

  /*
   * Every row's controls in the same place, which is the whole of what makes this a list
   * rather than six arrangements of the same four things.
   *
   * Two separate faults did it. A select takes its width from its widest option, and each of
   * these lists every root tag *except its own* — so "Comics", whose menu is the one without
   * "under Comics" in it, came out three pixels narrower and pushed its slider out of column.
   * And a nested row carried its indent on the row, so its menu and slider sat two dozen
   * pixels right of the five above. Measured in Chromium at the page's real width, all four
   * columns now have exactly one position across every row.
   *
   * Asserted as the classes that decide it, because jsdom does no layout — there are no
   * widths here to compare.
   */
  it("holds every row's controls in one column", async () => {
    renderWith(<TagsPage />, { "GET /api/tags": { body: [tech, nested] } });

    // A width of its own, rather than one taken from whichever options it happens to carry.
    const parent = await screen.findByLabelText("Where Tech sits");
    expect(parent.className).toContain("w-36");

    // The indent is inside the name's box, so the box is the same width either way and
    // nothing after it moves.
    const row = screen.getByLabelText("Name of AI & GPT").closest("div")!;
    expect(row.className).toContain("pl-6");
    expect(row.parentElement?.className).not.toContain("pl-6");
  });
});
