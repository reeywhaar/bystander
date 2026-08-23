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
    recovery_email: "",
    recovery_pending: "",
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
  it("will not change a password until both new ones agree", async () => {
    render(account());

    const button = await screen.findByRole("button", { name: "Change it" });
    expect(button).toBeDisabled();

    await userEvent.type(
      screen.getByLabelText("Current password"),
      "correct-horse",
    );
    await userEvent.type(
      screen.getByLabelText("New password"),
      "a-brand-new-one",
    );
    expect(button).toBeDisabled();

    await userEvent.type(
      screen.getByLabelText("New password again"),
      "a-brand-new-typo",
    );
    // The server receives one new password and cannot know it was typed twice, so this is
    // the only place the difference can be caught.
    expect(
      await screen.findByText("These two do not match."),
    ).toBeInTheDocument();
    expect(button).toBeDisabled();

    await userEvent.clear(screen.getByLabelText("New password again"));
    await userEvent.type(
      screen.getByLabelText("New password again"),
      "a-brand-new-one",
    );
    expect(button).toBeEnabled();
  });

  it("refuses a new password shorter than the server would take", async () => {
    render(account());

    await userEvent.type(
      await screen.findByLabelText("Current password"),
      "correct-horse",
    );
    await userEvent.type(screen.getByLabelText("New password"), "short");
    await userEvent.type(screen.getByLabelText("New password again"), "short");

    expect(screen.getByRole("button", { name: "Change it" })).toBeDisabled();
  });

  it("empties the password fields once it has changed one", async () => {
    const { transport } = render(account());

    await userEvent.type(
      await screen.findByLabelText("Current password"),
      "correct-horse",
    );
    await userEvent.type(
      screen.getByLabelText("New password"),
      "a-brand-new-one",
    );
    await userEvent.type(
      screen.getByLabelText("New password again"),
      "a-brand-new-one",
    );
    await userEvent.click(screen.getByRole("button", { name: "Change it" }));

    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/account/password")?.body,
      ).toEqual({
        current_password: "correct-horse",
        new_password: "a-brand-new-one",
      }),
    );
    // Three password boxes left full are three passwords to read over a shoulder.
    await waitFor(() =>
      expect(screen.getByLabelText("Current password")).toHaveValue(""),
    );
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("New password again")).toHaveValue("");
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
