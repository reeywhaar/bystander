import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWith } from "@app/test/harness";

import { SignInDialog } from "@app/components/SignInDialog";

const forgotten = { name: "Forgotten your password?" };

describe("SignInDialog", () => {
  it("offers a way back in where this instance can mail one", async () => {
    renderWith(<SignInDialog open onClose={() => {}} />, {
      "GET /api/instance": { body: { recovery: true } },
    });

    // Out of the island rather than into a route: /forgot is the login document, and this
    // dialog opens over the landing page or somebody's published page.
    const link = await screen.findByRole("link", forgotten);
    expect(link).toHaveAttribute("href", "/forgot");
  });

  it("offers nothing where it cannot", async () => {
    renderWith(<SignInDialog open onClose={() => {}} />, {
      "GET /api/instance": { body: { recovery: false } },
    });

    expect(await screen.findByLabelText("Name")).toBeInTheDocument();
    expect(screen.queryByRole("link", forgotten)).not.toBeInTheDocument();
  });

  /*
   * Both callers mount this permanently and toggle `open`, so an ungated query would ask
   * this on every published page a stranger opens — for a question nobody has asked yet.
   */
  it("asks nothing while it is shut", async () => {
    const { transport } = renderWith(
      <SignInDialog open={false} onClose={() => {}} />,
      { "GET /api/instance": { body: { recovery: true } } },
    );
    await waitFor(() => {
      expect(transport.calls).toEqual([]);
    });
  });
});
