import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { User } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { RecoveryDialog } from "@app/apps/admin/RecoveryDialog";

const alice: User = {
  id: "p_1",
  username: "alice",
  role: "user",
  created_at: 0,
  disabled_at: null,
  feed_count: 3,
};

describe("RecoveryDialog", () => {
  // Opening it is the decision. A form whose only control says "yes, the thing you just
  // asked for" is a step that exists to be clicked through.
  it("mints on open and shows the link", async () => {
    const { transport } = renderWith(
      <RecoveryDialog user={alice} onClose={() => {}} />,
      {
        "POST /api/admin/users/p_1/recovery": {
          status: 201,
          body: {
            url: "https://read.example.com/recover/abc",
            expires_at: 0,
            username: "alice",
          },
        },
      },
    );

    expect(
      await screen.findByText("https://read.example.com/recover/abc"),
    ).toBeInTheDocument();
    expect(transport.calls).toContainEqual({
      method: "POST",
      path: "/api/admin/users/p_1/recovery",
      body: undefined,
    });
  });

  // Shut means shut: a dialog that minted on mount rather than on open would hand out a link
  // every time the page holding it rendered.
  it("mints nothing while it is closed", () => {
    const { transport } = renderWith(
      <RecoveryDialog user={null} onClose={() => {}} />,
      {},
    );
    expect(transport.calls).toEqual([]);
  });

  it("says what the refusal was", async () => {
    renderWith(<RecoveryDialog user={alice} onClose={() => {}} />, {
      "POST /api/admin/users/p_1/recovery": {
        status: 409,
        body: { error: "that account is disabled" },
      },
    });
    expect(
      await screen.findByText("that account is disabled"),
    ).toBeInTheDocument();
  });
});
