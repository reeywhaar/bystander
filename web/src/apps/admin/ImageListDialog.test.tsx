import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { ImageFailure, UnmeasuredImage } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { ImageListDialog } from "@app/apps/admin/ImageListDialog";

function picture(overrides: Partial<UnmeasuredImage> = {}): UnmeasuredImage {
  return {
    url: "https://cdn.example.com/one.png",
    reason: "refused",
    retry_at: 0,
    articles: 1,
    title: "An article",
    ...overrides,
  };
}

const refused: ImageFailure = { reason: "refused", count: 2 };

function open(pictures: UnmeasuredImage[], limit = 100, failure = refused) {
  return renderWith(<ImageListDialog failure={failure} onClose={() => {}} />, {
    "GET /api/admin/images/unmeasured": {
      body: { reason: failure.reason, limit, pictures },
    },
    "POST /api/admin/images/retry": { body: { queued: 1 } },
  });
}

describe("ImageListDialog", () => {
  // The counts say what is wrong; this says with what. Forty under "refused" is either one
  // host with hotlink protection or forty publishers each losing one, and only the addresses
  // tell those apart — so the address is on the row, and it is a link.
  it("names the address behind each picture, and links to it", async () => {
    open([picture({ url: "https://cdn.example.com/a.png", title: "Bells" })]);

    const link = await screen.findByRole("link", {
      name: "https://cdn.example.com/a.png",
    });
    expect(link).toHaveAttribute("href", "https://cdn.example.com/a.png");
    expect(link).toHaveAttribute("target", "_blank");
    expect(screen.getByText("Bells")).toBeInTheDocument();
  });

  // A list shorter than the count in the title is the ordinary case, not a fault: the queue
  // measures some of them between the count being taken and the list being asked for. Saying
  // "showing 1 of 2" over a complete list of one reads as something going wrong.
  it("says nothing about truncation when the list is simply shorter", async () => {
    open([picture()], 100, { reason: "refused", count: 2 });

    await screen.findByRole("link", { name: picture().url });
    expect(screen.queryByText(/There are more/)).not.toBeInTheDocument();
  });

  it("says so when the list was actually cut short", async () => {
    open([picture(), picture({ url: "https://cdn.example.com/b.png" })], 2, {
      reason: "refused",
      count: 40,
    });

    expect(await screen.findByText(/There are more/)).toBeInTheDocument();
  });

  // Resetting one address and resetting the category are different decisions — a host that
  // changed against a build that did — so the request has to carry which was meant.
  it("resets one address without touching the rest of its category", async () => {
    const { transport } = open([
      picture({ url: "https://cdn.example.com/a.png" }),
    ]);

    await userEvent.click(await screen.findByRole("button", { name: "Reset" }));

    await waitFor(() => {
      const retry = transport.calls.find((c) =>
        c.path.endsWith("/images/retry"),
      );
      expect(retry?.body).toEqual({
        url: "https://cdn.example.com/a.png",
        reason: "",
      });
    });
  });

  it("resets the whole category from the footer", async () => {
    const { transport } = open([picture()]);

    await userEvent.click(
      await screen.findByRole("button", { name: "Reset all 2" }),
    );

    await waitFor(() => {
      const retry = transport.calls.find((c) =>
        c.path.endsWith("/images/retry"),
      );
      expect(retry?.body).toEqual({ url: "", reason: "refused" });
    });
  });

  // Nothing has asked about these yet, so they are already due and there is nothing to reset.
  it("offers no group reset for the pictures nothing has asked about", async () => {
    open([picture({ reason: "" })], 100, { reason: "", count: 1 });

    await screen.findByRole("link", { name: picture().url });
    expect(
      screen.queryByRole("button", { name: /Reset all/ }),
    ).not.toBeInTheDocument();
  });
});
