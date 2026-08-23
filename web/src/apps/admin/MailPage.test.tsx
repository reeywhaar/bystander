import { screen, waitFor } from "@testing-library/react";
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

describe("MailPage", () => {
  it("cannot be saved until the first password is typed", async () => {
    render(unconfigured);

    const save = await screen.findByRole("button", { name: "Save" });
    expect(save).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Host"), "smtp.example.com");
    await userEvent.type(screen.getByLabelText("Username"), "operator");
    await userEvent.type(screen.getByLabelText("From"), "paper@example.com");
    // Everything but the secret, which is the whole point of this being disabled.
    expect(save).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Password"), "hunter2");
    expect(save).toBeEnabled();
  });

  it("moves the port when the encryption changes, but not over a typed one", async () => {
    const { transport } = render(configured);

    const port = await screen.findByLabelText("Port");
    expect(port).toHaveValue(587);

    await userEvent.selectOptions(
      screen.getByLabelText("Encryption"),
      "implicit",
    );
    expect(port).toHaveValue(465);

    // A port somebody chose themselves survives a change of mind about encryption.
    await userEvent.clear(port);
    await userEvent.type(port, "2525");
    await userEvent.selectOptions(
      screen.getByLabelText("Encryption"),
      "starttls",
    );
    expect(port).toHaveValue(2525);

    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(transport.calls.find((c) => c.method === "PUT")).toBeDefined(),
    );
    expect(transport.calls.find((c) => c.method === "PUT")?.body).toMatchObject(
      { port: 2525, tls: "starttls" },
    );
  });

  it("sends an empty password to mean the stored one, and forgets what was typed", async () => {
    const { transport } = render(configured);

    // Nothing is prefilled here, because nothing is sent here.
    const password = await screen.findByLabelText("Password");
    expect(password).toHaveValue("");
    expect(password).toHaveAttribute("placeholder", "unchanged");

    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.method === "PUT")?.body,
      ).toMatchObject({ password: "" }),
    );

    await userEvent.type(password, "a-new-one");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(
        transport.calls.filter((c) => c.method === "PUT").at(-1)?.body,
      ).toMatchObject({ password: "a-new-one" }),
    );
    // Stored now, so holding on to it in the form would be holding it for nothing.
    await waitFor(() => expect(password).toHaveValue(""));
  });

  it("offers a test send only once there is something to send with", async () => {
    render(unconfigured);

    await screen.findByLabelText("Host");
    expect(screen.queryByLabelText("To")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Forget the relay" }),
    ).not.toBeInTheDocument();
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
