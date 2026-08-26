import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { renderWith } from "@app/test/harness";

import { DeleteAccountDialog } from "@app/apps/manage/DeleteAccountDialog";

const NOW = Math.floor(Date.now() / 1000);

function open(notified = true, status = 200) {
  return renderWith(<DeleteAccountDialog open onClose={() => {}} />, {
    "POST /api/account/deletion": {
      status,
      body:
        status === 200
          ? {
              deleted_at: NOW,
              purge_at: NOW + 7 * 86400,
              notified,
            }
          : { error: "that is not your password" },
    },
  });
}

describe("DeleteAccountDialog", () => {
  // The moment somebody decides to leave is the last moment the offer is any use.
  it("offers the export before anything is erased", () => {
    open();

    const link = screen.getByRole("link", { name: "Download your data" });
    expect(link).toHaveAttribute("href", "/api/account/export");
    expect(link).toHaveAttribute("download");
  });

  // Being signed in is not the same as knowing the password.
  it("will not ask until a password is typed", async () => {
    const { transport } = open();
    const user = userEvent.setup();

    const button = screen.getByRole("button", { name: "Delete my account" });
    expect(button).toBeDisabled();

    await user.type(screen.getByLabelText("Your password"), "hunter2");
    expect(button).toBeEnabled();
    expect(
      transport.calls.some((c) => c.path === "/api/account/deletion"),
    ).toBe(false);
  });

  // Nothing is erased today, and the one thing that undoes it costs nothing to say.
  it("says when it happens and how to stop it", async () => {
    open();
    const user = userEvent.setup();

    expect(screen.getByText(/Nothing is erased today/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Your password"), "hunter2");
    await user.click(screen.getByRole("button", { name: "Delete my account" }));

    expect(
      await screen.findByText(/Your account will be erased on/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Signing in again before then/),
    ).toBeInTheDocument();
  });

  // An account with no address on file has no safety net, and the interface says so rather
  // than implying the better of the two.
  it("says plainly when nothing could be sent", async () => {
    open(false);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Your password"), "hunter2");
    await user.click(screen.getByRole("button", { name: "Delete my account" }));

    expect(
      await screen.findByText(/no recovery address on this account/),
    ).toBeInTheDocument();
  });

  it("says so when the password is refused, and erases nothing", async () => {
    open(true, 400);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Your password"), "wrong");
    await user.click(screen.getByRole("button", { name: "Delete my account" }));

    expect(
      await screen.findByText("that is not your password"),
    ).toBeInTheDocument();
    // Still the form, not the confirmation.
    expect(
      screen.queryByText(/Your account will be erased on/),
    ).not.toBeInTheDocument();
  });

  // Every session ended with the request, including this one, so there is nowhere in the
  // application left to be — and only the server decides what a visitor without a cookie
  // gets at "/".
  it("leaves the application once the request is in", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });

    open();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    await user.type(screen.getByLabelText("Your password"), "hunter2");
    await user.click(screen.getByRole("button", { name: "Delete my account" }));
    await screen.findByText(/Your account will be erased on/);

    await vi.advanceTimersByTimeAsync(6000);
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/"));

    vi.unstubAllGlobals();
    vi.useRealTimers();
  });
});
