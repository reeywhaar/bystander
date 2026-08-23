import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { SmtpConfig } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { MailPage } from "@app/apps/admin/MailPage";

const unconfigured: SmtpConfig = {
  configured: false,
  host: "",
  port: 587,
  tls: "starttls",
  username: "",
  from_address: "",
  sender_name: "",
  updated_at: 0,
};

const configured: SmtpConfig = {
  configured: true,
  host: "smtp.example.com",
  port: 587,
  tls: "starttls",
  username: "operator",
  from_address: "paper@example.com",
  sender_name: "Rundschau",
  updated_at: Math.floor(Date.now() / 1000) - 3600,
};

const render = (config: SmtpConfig) =>
  renderWith(<MailPage />, {
    "GET /api/admin/smtp": { body: config },
    "PUT /api/admin/smtp": { body: { ...config, configured: true } },
    "POST /api/admin/smtp/test": { status: 204 },
    "DELETE /api/admin/smtp": { status: 204 },
  });

const dialog = () => screen.getByRole("dialog");

describe("MailPage", () => {
  it("states what is configured rather than offering it as a form", async () => {
    render(configured);

    // Everything that matters about a relay, on the page, without opening anything.
    expect(await screen.findByText("smtp.example.com:587")).toBeInTheDocument();
    expect(screen.getByText("Rundschau")).toBeInTheDocument();
    expect(screen.getByText("<paper@example.com>")).toBeInTheDocument();
    expect(screen.getByText("operator")).toBeInTheDocument();

    // The fields are not sitting on the page waiting to be edited by accident.
    expect(screen.queryByLabelText("Host")).not.toBeInTheDocument();
  });

  it("offers nothing to send with until a relay is set up", async () => {
    render(unconfigured);

    expect(
      await screen.findByText(/Nothing is configured, so nothing is sent./),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("To")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Forget the relay" }),
    ).not.toBeInTheDocument();
  });

  it("tries what is typed without saving it", async () => {
    const { transport } = render(configured);

    await userEvent.click(
      await screen.findByRole("button", { name: "Change" }),
    );
    const host = within(dialog()).getByLabelText("Host");
    await userEvent.clear(host);
    await userEvent.type(host, "smtp.other.example");
    await userEvent.type(
      within(dialog()).getByLabelText("Try it first"),
      "me@example.com",
    );
    await userEvent.click(
      within(dialog()).getByRole("button", { name: "Send a test" }),
    );

    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/admin/smtp/test")?.body,
      ).toMatchObject({
        to: "me@example.com",
        // What was typed, not what is stored — that is the whole point of testing here.
        relay: { host: "smtp.other.example", password: "" },
      }),
    );
    // And nothing was written on the way.
    expect(transport.calls.some((c) => c.method === "PUT")).toBe(false);
  });

  it("cannot be saved or tried until the first password is typed", async () => {
    render(unconfigured);

    await userEvent.click(
      await screen.findByRole("button", { name: "Set up a relay" }),
    );
    const box = dialog();
    await userEvent.type(
      within(box).getByLabelText("Host"),
      "smtp.example.com",
    );
    await userEvent.type(within(box).getByLabelText("Username"), "operator");
    await userEvent.type(
      within(box).getByLabelText("From"),
      "paper@example.com",
    );
    await userEvent.type(
      within(box).getByLabelText("Try it first"),
      "me@example.com",
    );

    // Everything but the secret. There is nothing stored to fall back on, so neither
    // button can do anything useful yet.
    expect(within(box).getByRole("button", { name: "Save" })).toBeDisabled();
    expect(
      within(box).getByRole("button", { name: "Send a test" }),
    ).toBeDisabled();

    await userEvent.type(within(box).getByLabelText("Password"), "hunter2");
    expect(within(box).getByRole("button", { name: "Save" })).toBeEnabled();
    expect(
      within(box).getByRole("button", { name: "Send a test" }),
    ).toBeEnabled();
  });

  it("sends an empty password to mean the stored one", async () => {
    const { transport } = render(configured);

    await userEvent.click(
      await screen.findByRole("button", { name: "Change" }),
    );
    // Nothing is prefilled here, because nothing is ever sent to the browser.
    const password = within(dialog()).getByLabelText("Password");
    expect(password).toHaveValue("");
    expect(password).toHaveAttribute("placeholder", "unchanged");

    await userEvent.click(
      within(dialog()).getByRole("button", { name: "Save" }),
    );
    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.method === "PUT")?.body,
      ).toMatchObject({ host: "smtp.example.com", password: "" }),
    );
  });

  it("moves the port when the encryption changes, but not over a typed one", async () => {
    render(configured);

    await userEvent.click(
      await screen.findByRole("button", { name: "Change" }),
    );
    const box = dialog();
    const port = within(box).getByLabelText("Port");
    expect(port).toHaveValue(587);

    await userEvent.selectOptions(
      within(box).getByLabelText("Encryption"),
      "implicit",
    );
    expect(port).toHaveValue(465);

    // A port somebody chose themselves survives a change of mind about encryption.
    await userEvent.clear(port);
    await userEvent.type(port, "2525");
    await userEvent.selectOptions(
      within(box).getByLabelText("Encryption"),
      "starttls",
    );
    expect(port).toHaveValue(2525);
  });

  it("discards what was typed when the dialog is cancelled", async () => {
    render(configured);

    await userEvent.click(
      await screen.findByRole("button", { name: "Change" }),
    );
    await userEvent.type(within(dialog()).getByLabelText("Host"), "-typo");
    await userEvent.click(
      within(dialog()).getByRole("button", { name: "Cancel" }),
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "Change" }),
    );
    expect(within(dialog()).getByLabelText("Host")).toHaveValue(
      "smtp.example.com",
    );
  });

  it("says what the relay said when it refuses", async () => {
    const { transport } = render(configured);
    transport.recording["POST /api/admin/smtp/test"] = {
      status: 502,
      body: { error: "the relay refused the sender: 550 relaying denied" },
    };

    await userEvent.type(await screen.findByLabelText("To"), "me@example.com");
    await userEvent.click(screen.getByRole("button", { name: "Send" }));

    // The relay's own words, not "sending failed": the operator has to know which of
    // three things went wrong.
    expect(await screen.findByText(/550 relaying denied/)).toBeInTheDocument();
  });
});
