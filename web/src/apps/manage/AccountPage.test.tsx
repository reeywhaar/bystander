import { screen, waitFor } from "@testing-library/react";
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
    mail_configured: true,
    ...overrides,
  };
}

const render = (data: Account) =>
  renderWith(<AccountPage />, {
    "GET /api/account": { body: data },
    "PATCH /api/account": {
      body: { ...data, recovery_email: "alice@example.com" },
    },
    "POST /api/account/password": { status: 204 },
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

  it("says so when nothing could be sent to a recovery address", async () => {
    render(account({ mail_configured: false }));

    // Storing an address against an instance that cannot send is a promise nobody can
    // keep, and the moment somebody finds that out is the moment they are locked out.
    expect(
      await screen.findByText(/No mail relay is configured/),
    ).toBeInTheDocument();
  });

  it("says nothing about mail when a relay exists", async () => {
    render(account());

    await screen.findByLabelText("Address");
    expect(
      screen.queryByText(/No mail relay is configured/),
    ).not.toBeInTheDocument();
  });

  it("saves a recovery address, and offers to remove one", async () => {
    const { transport } = render(
      account({ recovery_email: "alice@example.com" }),
    );

    // Prefilled from what is stored, and saving an unchanged address is not a request.
    const field = await screen.findByLabelText("Address");
    expect(field).toHaveValue("alice@example.com");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await userEvent.clear(field);
    // An empty box means removing it, and the button says so rather than saying "Save"
    // and quietly deleting something.
    expect(
      await screen.findByRole("button", { name: "Remove it" }),
    ).toBeEnabled();

    await userEvent.type(field, "alice@example.org");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(transport.calls.find((c) => c.method === "PATCH")?.body).toEqual({
        recovery_email: "alice@example.org",
      }),
    );
  });
});
