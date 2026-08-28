import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Tag } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { TagsPage } from "@app/apps/manage/TagsPage";

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
});
