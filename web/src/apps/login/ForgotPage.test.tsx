import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderWith } from "@app/test/harness";

import { ForgotPage } from "@app/apps/login/ForgotPage";

async function ask(address: string) {
  const rendered = renderWith(<ForgotPage />, {
    "POST /api/recoveries": { status: 204 },
  });
  await userEvent.type(screen.getByLabelText("Address"), address);
  await userEvent.click(screen.getByRole("button", { name: "Send me a link" }));
  return rendered;
}

describe("ForgotPage", () => {
  it("asks for the address and says to look in the inbox", async () => {
    const { transport } = await ask("alice@example.com");

    expect(await screen.findByText("Look in your inbox")).toBeInTheDocument();
    expect(transport.calls).toContainEqual({
      method: "POST",
      path: "/api/recoveries",
      body: { email: "alice@example.com" },
    });
  });

  /*
   * The property the whole flow rests on.
   *
   * The server answers 204 whether or not an account has that address, and this page must
   * not undo that by rendering something a stranger could tell apart. So the confirmation
   * is written from what was typed and nothing else — there is nothing in the response to
   * render, which is the point.
   */
  it("says the same thing for an address that reaches nobody", async () => {
    await ask("nobody@example.com");

    expect(await screen.findByText("Look in your inbox")).toBeInTheDocument();
    expect(screen.getByText("nobody@example.com")).toBeInTheDocument();
  });

  // Somebody who never set a recovery address cannot learn that from this form, so the other
  // way in is named rather than left to be discovered.
  it("points at the administrator as the other way in", () => {
    renderWith(<ForgotPage />, {});
    expect(screen.getByText(/runs this\s+instance/)).toBeInTheDocument();
  });
});
