import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { AdminInvite, SmtpConfig } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { InviteDialog } from "@app/apps/admin/InviteDialog";

const relay: SmtpConfig = {
  configured: true,
  host: "smtp.example.com",
  port: 587,
  tls: "starttls",
  username: "operator",
  from_address: "paper@example.com",
  sender_name: "",
  updated_at: 0,
};

function invite(overrides: Partial<AdminInvite> = {}): AdminInvite {
  return {
    id: "i_1",
    role: "user",
    created_at: 0,
    expires_at: 0,
    email: "",
    accepted_at: null,
    username: "",
    ...overrides,
  };
}

function open(smtp: SmtpConfig, made: AdminInvite) {
  return renderWith(<InviteDialog open onClose={() => {}} />, {
    "GET /api/admin/smtp": { body: smtp },
    "POST /api/admin/invites": { body: made },
  });
}

describe("InviteDialog", () => {
  // The link and the address are exclusive, and this is the half that hands a link back.
  it("hands back the link when the invitation is one to pass on", async () => {
    const { transport } = open(
      relay,
      invite({ url: "https://read.example.com/invite/abc" }),
    );

    await userEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(
      await screen.findByText("https://read.example.com/invite/abc"),
    ).toBeInTheDocument();
    const sent = transport.calls.find((c) => c.path === "/api/admin/invites");
    expect(sent?.body).toEqual({ role: "user", email: "" });
  });

  it("carries the role the segmented control is on", async () => {
    const { transport } = open(relay, invite({ role: "admin", url: "x" }));

    await userEvent.click(screen.getByRole("radio", { name: "Administrator" }));
    await userEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      const sent = transport.calls.find((c) => c.path === "/api/admin/invites");
      expect(sent?.body).toEqual({ role: "admin", email: "" });
    });
  });

  // The other half: an address goes up, and no link comes back. That omission is what makes
  // accepting the invitation proof of the address.
  it("sends to an address, and shows no link afterwards", async () => {
    const { transport } = open(relay, invite({ email: "them@example.com" }));

    await userEvent.click(screen.getByRole("radio", { name: "Email" }));
    await userEvent.type(
      screen.getByLabelText("Send it to"),
      "them@example.com",
    );
    await userEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByText(/Sent to/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Copy" }),
    ).not.toBeInTheDocument();

    const sent = transport.calls.find((c) => c.path === "/api/admin/invites");
    expect(sent?.body).toEqual({ role: "user", email: "them@example.com" });
  });

  // Reached by choosing to send, not by finding the control dead. A greyed button that says
  // nothing leaves somebody looking for the reason in the wrong place.
  it("says why it cannot send when no relay is configured", async () => {
    open({ ...relay, configured: false }, invite());

    // Nothing about mail until somebody asks for it.
    expect(screen.queryByText(/No mail relay/)).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("radio", { name: "Email" }));

    expect(
      await screen.findByText(/No mail relay is configured/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();
  });
});
