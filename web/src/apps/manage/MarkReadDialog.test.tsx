import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Subscription } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { MarkReadDialog } from "@app/apps/manage/MarkReadDialog";

const feed: Subscription = {
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
  created_at: 0,
  last_success_at: null,
  last_status: 200,
  last_error: "",
  last_error_body: "",
  failure_count: 0,
};

describe("MarkReadDialog", () => {
  // The thing somebody needs to know before pressing it, and the thing they cannot see: this
  // reaches articles no page has shown, so what is marked never turns up later.
  it("says that it reaches further than the page on screen", () => {
    renderWith(<MarkReadDialog feed={feed} open onClose={vi.fn()} />, {});
    expect(
      screen.getByText(/articles no page has shown you yet/),
    ).toBeInTheDocument();
  });

  it("sends the span that was chosen", async () => {
    const { transport } = renderWith(
      <MarkReadDialog feed={feed} open onClose={vi.fn()} />,
      { "POST /api/feeds/s_1/read": { body: { marked: 12 } } },
    );

    await userEvent.click(screen.getByLabelText(/Older than a month/));
    await userEvent.click(screen.getByRole("button", { name: "Mark read" }));

    await waitFor(() => {
      expect(screen.getByText("Marked 12 articles read.")).toBeInTheDocument();
    });
    expect(transport.calls).toContainEqual(
      expect.objectContaining({
        method: "POST",
        path: "/api/feeds/s_1/read",
        body: { older_than: "month" },
      }),
    );
  });

  // Everything is the empty span, not a bound of zero.
  it("sends nothing for everything", async () => {
    const { transport } = renderWith(
      <MarkReadDialog feed={feed} open onClose={vi.fn()} />,
      { "POST /api/feeds/s_1/read": { body: { marked: 1 } } },
    );

    await userEvent.click(screen.getByLabelText(/Everything/));
    await userEvent.click(screen.getByRole("button", { name: "Mark read" }));

    await waitFor(() => {
      expect(screen.getByText("Marked 1 article read.")).toBeInTheDocument();
    });
    expect(transport.calls).toContainEqual(
      expect.objectContaining({ body: { older_than: "" } }),
    );
  });

  // The other direction, which the option list offers alongside the spans.
  it("forgets the feed's read state when that is what was chosen", async () => {
    const { transport } = renderWith(
      <MarkReadDialog feed={feed} open onClose={vi.fn()} />,
      { "DELETE /api/feeds/s_1/read": { body: { marked: 8 } } },
    );

    await userEvent.click(screen.getByLabelText(/Mark it all unread/));
    // The button follows the choice rather than staying "Mark read" over an option that
    // does the opposite.
    await userEvent.click(screen.getByRole("button", { name: "Mark unread" }));

    await waitFor(() => {
      expect(screen.getByText("Marked 8 articles unread.")).toBeInTheDocument();
    });
    expect(transport.calls).toContainEqual(
      expect.objectContaining({
        method: "DELETE",
        path: "/api/feeds/s_1/read",
      }),
    );
  });

  // Nothing is a real outcome of the same press, and it covers two cases the server does not
  // tell apart: nothing that old, or it had been read already.
  it("says so when there was nothing to do", async () => {
    renderWith(<MarkReadDialog feed={feed} open onClose={vi.fn()} />, {
      "POST /api/feeds/s_1/read": { body: { marked: 0 } },
    });

    await userEvent.click(screen.getByRole("button", { name: "Mark read" }));
    await waitFor(() => {
      expect(screen.getByText(/Nothing to mark/)).toBeInTheDocument();
    });
  });
});
