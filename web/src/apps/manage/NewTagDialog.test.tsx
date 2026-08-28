import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Tag } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { NewTagDialog } from "@app/apps/manage/NewTagDialog";

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

const made: Tag = {
  id: "t_new",
  name: "Long reads",
  parent_id: "t_news",
  priority: 20,
  created_at: 0,
};

describe("NewTagDialog", () => {
  it("makes a tag with all three decisions at once", async () => {
    const onCreated = vi.fn();
    const { transport } = renderWith(
      <NewTagDialog
        open
        tags={[news, world]}
        onClose={() => {}}
        onCreated={onCreated}
      />,
      { "POST /api/tags": { status: 201, body: made } },
    );

    await userEvent.type(screen.getByLabelText("Name"), "Long reads");
    await userEvent.selectOptions(
      screen.getByLabelText("Where it sits"),
      "t_news",
    );
    await userEvent.click(screen.getByRole("button", { name: "Make it" }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.path === "/api/tags");
      expect(sent?.body).toEqual({
        name: "Long reads",
        parent_id: "t_news",
        priority: 50,
      });
    });
    // Handed back, because whoever opened this usually wants to do something with it —
    // filing a feed opens it precisely because the tag it wants does not exist.
    expect(onCreated).toHaveBeenCalledWith(made);
  });

  // The list nests one deep and the server refuses a cycle. This keeps the impossible
  // choices off the menu rather than letting somebody find out by pressing Make it.
  it("offers only roots as a parent", () => {
    renderWith(
      <NewTagDialog open tags={[news, world]} onClose={() => {}} />,
      {},
    );

    const where = screen.getByLabelText("Where it sits");
    expect(
      within(where).getByRole("option", { name: "under News" }),
    ).toBeInTheDocument();
    expect(
      within(where).queryByRole("option", { name: "under World" }),
    ).toBeNull();
  });

  it("will not make one with no name", async () => {
    const { transport } = renderWith(
      <NewTagDialog open tags={[]} onClose={() => {}} />,
      { "POST /api/tags": { status: 201, body: made } },
    );

    expect(screen.getByRole("button", { name: "Make it" })).toBeDisabled();
    await userEvent.type(screen.getByLabelText("Name"), "   ");
    expect(screen.getByRole("button", { name: "Make it" })).toBeDisabled();
    expect(transport.calls).toEqual([]);
  });

  it("says what the refusal was", async () => {
    renderWith(<NewTagDialog open tags={[]} onClose={() => {}} />, {
      "POST /api/tags": {
        status: 409,
        body: { error: "you already have a tag called that" },
      },
    });

    await userEvent.type(screen.getByLabelText("Name"), "News");
    await userEvent.click(screen.getByRole("button", { name: "Make it" }));

    expect(
      await screen.findByText("you already have a tag called that"),
    ).toBeInTheDocument();
  });
});
