import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Article } from "@app/api/types";
import { renderWith, type Recording } from "@app/test/harness";

import { FeedActionsDialog } from "@app/apps/reader/FeedActionsDialog";

function article(priority = 50, subscriptionID = "s_1"): Article {
  return {
    id: "a_1",
    rank: 0,
    slot: "standard",
    read_at: null,
    title: "A story about a thing",
    link: "https://example.com/1",
    author: "",
    summary: "",
    image_url: "",
    image_width: 0,
    image_height: 0,
    published_at: 0,
    feed: {
      id: "f_1",
      title: "The Example",
      site_url: "https://example.com",
      subscription_id: subscriptionID,
      priority,
    },
  };
}

const written: Recording = {
  "PATCH /api/feeds/s_1": { status: 204 },
  "PUT /api/edition/items/a_1/read": { status: 204 },
  "POST /api/feeds/s_1/read": { body: { marked: 4 } },
  "DELETE /api/feeds/s_1": { status: 204 },
};

function open(item = article(), recording: Recording = written) {
  const onClose = vi.fn();
  return {
    onClose,
    ...renderWith(
      <FeedActionsDialog article={item} onClose={onClose} />,
      recording,
    ),
  };
}

describe("FeedActionsDialog", () => {
  // A step that does not change the word is a button somebody presses and then waits a day
  // to find out whether it did anything.
  it("says where the feed stands and where a press would put it", () => {
    open();
    expect(screen.getByText("as usual")).toBeInTheDocument();
    expect(
      screen.getByText("Drawn more often from now on."),
    ).toBeInTheDocument();
  });

  it("shows more without touching the article", async () => {
    const { transport } = open();

    await userEvent.click(screen.getByRole("button", { name: /Show more/ }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toEqual({ priority: 70 });
    });
    // Wanting more of a source says nothing about having finished with this one.
    expect(transport.calls.some((call) => call.method === "PUT")).toBe(false);
  });

  /*
   * "Less of this" is said about something you have finished with, so leaving it unread
   * would be asking to be shown the very article that prompted it.
   */
  it("shows less and marks the article read", async () => {
    const { transport } = open();

    await userEvent.click(screen.getByRole("button", { name: /Show less/ }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toEqual({ priority: 30 });
    });
    expect(transport.calls).toContainEqual({
      method: "PUT",
      path: "/api/edition/items/a_1/read",
      body: undefined,
    });
  });

  it("does not mark an article that was already read", async () => {
    const { transport } = open({ ...article(), read_at: 1 });

    await userEvent.click(screen.getByRole("button", { name: /Show less/ }));

    await waitFor(() =>
      expect(transport.calls.some((call) => call.method === "PATCH")).toBe(
        true,
      ),
    );
    expect(transport.calls.some((call) => call.method === "PUT")).toBe(false);
  });

  it("keeps a priority inside its bounds", async () => {
    const { transport } = open(article(90));

    await userEvent.click(screen.getByRole("button", { name: /Show more/ }));

    await waitFor(() => {
      const sent = transport.calls.find((call) => call.method === "PATCH");
      expect(sent?.body).toEqual({ priority: 100 });
    });
  });

  // The one thing here that cannot be undone, so it is asked rather than done.
  it("asks before removing, and writes nothing until it is answered", async () => {
    const { transport } = open();

    await userEvent.click(screen.getByRole("button", { name: /Remove feed/ }));

    expect(screen.getByText(/Stop following/)).toBeInTheDocument();
    expect(transport.calls).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Keep it" }));
    expect(screen.queryByText(/Stop following/)).toBeNull();
    expect(transport.calls).toEqual([]);
  });

  /*
   * Marked read *then* unfollowed, in that order. The cards it already put on the live page
   * stay there — an edition is composed once — so without the marking, being done with a
   * feed leaves its articles sitting on the page looking unread.
   */
  it("marks everything read and then unfollows", async () => {
    const { transport, onClose } = open();

    await userEvent.click(screen.getByRole("button", { name: /Remove feed/ }));
    await userEvent.click(screen.getByRole("button", { name: "Remove it" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(
      transport.calls.map((call) => `${call.method} ${call.path}`),
    ).toEqual(["POST /api/feeds/s_1/read", "DELETE /api/feeds/s_1"]);
    // Everything, which is the empty span rather than a bound of zero.
    expect(transport.calls[0]?.body).toEqual({ older_than: "" });
  });

  // An article whose subscription went while the page was live. The card keeps its place,
  // and there is nothing left to act on.
  it("offers nothing for a feed no longer followed", () => {
    open(article(50, ""));

    expect(screen.getByText(/no longer follow this one/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Show more/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Remove feed/ })).toBeNull();
  });
});
