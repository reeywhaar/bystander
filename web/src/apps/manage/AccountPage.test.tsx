import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Account } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { AccountPage } from "@app/apps/manage/AccountPage";

function account(overrides: Partial<Account> = {}): Account {
  return {
    username: "alice",
    role: "user",
    created_at: Math.floor(Date.now() / 1000) - 86400 * 30,
    public_name: "",
    public_pages: true,
    public_indexing: false,
    recovery_email: "",
    recovery_pending: "",
    deletion_cancelled_at: 0,
    last_admin: false,
    mail_configured: true,
    ...overrides,
  };
}

const render = (data: Account) =>
  renderWith(<AccountPage />, {
    "GET /api/account": { body: data },
    "POST /api/account/password": { status: 204 },
    "POST /api/account/recovery": { status: 204 },
    "POST /api/account/recovery/confirm": {
      body: {
        ...data,
        recovery_email: "alice@example.org",
        recovery_pending: "",
      },
    },
    "DELETE /api/account/recovery": { status: 204 },
  });

describe("AccountPage", () => {
  /*
   * Behind a button, not open on the page.
   *
   * Everything else here is something to read — your name, where your pages are published,
   * the address you could be recovered through. Three empty password boxes in the middle of
   * that are the only part that looks like work outstanding, and they are three password
   * boxes on screen for as long as the page is. What the dialog itself does is its own test.
   */
  it("keeps the password fields behind a button", async () => {
    render(account());

    await screen.findByRole("button", { name: "Change password" });
    expect(screen.queryByLabelText("Current password")).toBeNull();

    await userEvent.click(
      screen.getByRole("button", { name: "Change password" }),
    );
    expect(screen.getByLabelText("Current password")).toBeInTheDocument();
  });

  it("offers no way to add an address when nothing could be sent to it", async () => {
    render(account({ mail_configured: false }));

    // An address cannot be proved without a relay, and a button that fails for reasons the
    // person pressing it cannot do anything about is worse than no button.
    expect(
      await screen.findByText(/No mail relay is configured/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Add an address" }),
    ).toBeDisabled();
  });

  it("says nothing about mail when a relay exists", async () => {
    render(account());

    await screen.findByRole("button", { name: "Add an address" });
    expect(
      screen.queryByText(/No mail relay is configured/),
    ).not.toBeInTheDocument();
  });

  it("puts nothing on record until the code comes back", async () => {
    const { transport } = render(account());

    expect(
      await screen.findByText(/None. Without one, a forgotten password/),
    ).toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("button", { name: "Add an address" }),
    );
    await userEvent.type(
      within(screen.getByRole("dialog")).getByLabelText("Address"),
      "alice@example.org",
    );
    await userEvent.click(screen.getByRole("button", { name: "Send a code" }));

    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/account/recovery")?.body,
      ).toEqual({ email: "alice@example.org" }),
    );
    // Sending a code changes nothing about the account. Only the second step does.
    expect(
      transport.calls.some((c) => c.path === "/api/account/recovery/confirm"),
    ).toBe(false);

    const box = screen.getByRole("dialog");
    // The address locks once a code is away: changing it would leave the code pointing at
    // the old one, which reads as the flow being further along than it is.
    expect(within(box).getByLabelText("Address")).toBeDisabled();
    expect(
      within(box).getByText(/A code was sent to alice@example.org/),
    ).toBeInTheDocument();

    // A short code is not a code. Eight is what the server generates.
    await userEvent.type(within(box).getByLabelText("Code"), "K7QM");
    expect(within(box).getByRole("button", { name: "Confirm" })).toBeDisabled();

    await userEvent.type(within(box).getByLabelText("Code"), "2XPT");
    await userEvent.click(within(box).getByRole("button", { name: "Confirm" }));

    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/account/recovery/confirm")
          ?.body,
      ).toEqual({ code: "K7QM2XPT" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("resumes an attempt already in flight rather than starting it again", async () => {
    render(account({ recovery_pending: "alice@example.org" }));

    expect(
      await screen.findByText(/Waiting on a code sent to alice@example.org/),
    ).toBeInTheDocument();

    // Reopening lands on the code, not back at the address: somebody is already holding a
    // code, and sending a second would waste the one they have.
    await userEvent.click(
      screen.getByRole("button", { name: "Finish confirming" }),
    );
    const box = screen.getByRole("dialog");
    expect(within(box).getByLabelText("Address")).toHaveValue(
      "alice@example.org",
    );
    expect(within(box).getByLabelText("Code")).toBeInTheDocument();
    expect(
      within(box).getByRole("button", { name: "Send it again" }),
    ).toBeInTheDocument();
  });

  it("offers to forget an address, and an attempt that never finished", async () => {
    const { transport } = render(
      account({ recovery_pending: "alice@example.org" }),
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "Remove it" }),
    );
    await waitFor(() =>
      expect(
        transport.calls.some(
          (c) => c.method === "DELETE" && c.path === "/api/account/recovery",
        ),
      ).toBe(true),
    );
  });
});

/*
 * The name somebody's published pages will live under.
 *
 * A second name, not the username: a username is a credential half the world reuses, and
 * publishing a page is no reason to oblige anybody to announce theirs.
 */
describe("AccountPage, the export", () => {
  // A link and not a button. The archive is streamed as it is read, so handing it to the
  // browser means neither side holds the whole thing — and a fetch-to-blob would give back
  // exactly the memory the endpoint was written to avoid.
  it("hands the download to the browser rather than fetching it", async () => {
    const { transport } = render(account());

    const link = await screen.findByRole("link", {
      name: "Download your data",
    });
    expect(link).toHaveAttribute("href", "/api/account/export");
    expect(link).toHaveAttribute("download");
    expect(transport.calls.some((c) => c.path === "/api/account/export")).toBe(
      false,
    );
  });

  // The unread half is bounded by how long articles are kept, and somebody who is not told
  // that concludes their reader lost the rest.
  it("says how far back each half reaches", async () => {
    render(account());

    expect(await screen.findByText(/thirty days/)).toBeInTheDocument();
    expect(
      screen.getByText(/as far as you have followed the feed/),
    ).toBeInTheDocument();
  });
});

describe("AccountPage, the public name", () => {
  it("says there is none, and asks for one", async () => {
    const { transport } = renderWith(<AccountPage />, {
      "GET /api/account": { body: account() },
      "PUT /api/account/public-name": {
        body: account({ public_name: "misha" }),
      },
    });

    expect(await screen.findByText(/None yet/)).toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("button", { name: "Choose a name" }),
    );
    // Typed however somebody likes, and tidied into something that can sit in a URL.
    await userEvent.type(
      await screen.findByLabelText("Public name"),
      "Misha V",
    );
    expect(
      screen.getByText("Your pages will be at /p/misha-v/…"),
    ).toBeInTheDocument();

    const dialog = screen.getByText("Choose a public name").closest("dialog")!;
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/account/public-name")
          ?.body,
      ).toEqual({ name: "misha-v" }),
    );
  });

  // Nothing stores the address — it is built from the name each time — so changing the name
  // moves every published page, and every link anybody already has stops working.
  it("warns that changing it moves every published page", async () => {
    renderWith(<AccountPage />, {
      "GET /api/account": { body: account({ public_name: "misha" }) },
    });

    await userEvent.click(
      await screen.findByRole("button", { name: "Change name" }),
    );
    const field = await screen.findByLabelText("Public name");
    expect(screen.queryByText(/Links to the old address/)).toBeNull();

    await userEvent.clear(field);
    await userEvent.type(field, "mv");
    expect(
      await screen.findByText(/Links to the old address/),
    ).toBeInTheDocument();
  });

  it("gives the name up", async () => {
    const { transport } = renderWith(<AccountPage />, {
      "GET /api/account": { body: account({ public_name: "misha" }) },
      "PUT /api/account/public-name": { body: account() },
    });

    await userEvent.click(
      await screen.findByRole("button", { name: "Give it up" }),
    );
    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/account/public-name")
          ?.body,
      ).toEqual({ name: "" }),
    );
  });
});
