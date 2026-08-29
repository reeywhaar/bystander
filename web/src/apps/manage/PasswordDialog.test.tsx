import { screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { renderWith, type Recording } from "@app/test/harness";

import { PasswordDialog } from "@app/apps/manage/PasswordDialog";

const written: Recording = { "POST /api/account/password": { status: 204 } };

function open(recording: Recording = written) {
  const onClose = vi.fn();
  return {
    onClose,
    ...renderWith(<PasswordDialog open onClose={onClose} />, recording),
  };
}

async function fill(current: string, next: string, again = next) {
  await userEvent.type(screen.getByLabelText("Current password"), current);
  await userEvent.type(screen.getByLabelText("New password"), next);
  await userEvent.type(screen.getByLabelText("New password again"), again);
}

describe("PasswordDialog", () => {
  it("will not change a password until both new ones agree", async () => {
    open();

    const button = screen.getByRole("button", { name: "Change it" });
    expect(button).toBeDisabled();

    await fill("correct-horse", "a-brand-new-one", "a-brand-new-typo");

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
    open();
    await fill("correct-horse", "short");
    expect(screen.getByRole("button", { name: "Change it" })).toBeDisabled();
  });

  /*
   * Being signed in is not the same as knowing the password, and the difference is what
   * stops a borrowed session becoming a taken account. Behind a button it is the same check,
   * so the field is still required.
   */
  it("still requires the current password", async () => {
    open();
    await userEvent.type(
      screen.getByLabelText("New password"),
      "a-brand-new-one",
    );
    await userEvent.type(
      screen.getByLabelText("New password again"),
      "a-brand-new-one",
    );
    expect(screen.getByRole("button", { name: "Change it" })).toBeDisabled();
  });

  it("sends both passwords and closes saying it changed one", async () => {
    const { transport, onClose } = open();

    await fill("correct-horse", "a-brand-new-one");
    await userEvent.click(screen.getByRole("button", { name: "Change it" }));

    await waitFor(() =>
      expect(
        transport.calls.find((c) => c.path === "/api/account/password")?.body,
      ).toEqual({
        current_password: "correct-horse",
        new_password: "a-brand-new-one",
      }),
    );
    // True, so the page can say so where somebody is left looking — a confirmation inside a
    // box that closes on the same press is a confirmation nobody reads.
    await waitFor(() => expect(onClose).toHaveBeenCalledWith(true));
  });

  /*
   * Three password boxes left full are three passwords to read over a shoulder — so what was
   * typed and abandoned must not be there the next time it opens.
   *
   * Driven through a caller rather than by re-rendering the dialog directly, because the
   * harness wraps what it is given in the providers and a bare rerender would replace them.
   */
  it("keeps nothing that was typed and abandoned", async () => {
    function Caller() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            Reopen
          </button>
          <PasswordDialog open={open} onClose={() => setOpen(false)} />
        </>
      );
    }
    renderWith(<Caller />, written);

    await fill("correct-horse", "a-brand-new-one");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await userEvent.click(screen.getByRole("button", { name: "Reopen" }));

    expect(screen.getByLabelText("Current password")).toHaveValue("");
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("New password again")).toHaveValue("");
  });

  it("says what the refusal was, and stays open", async () => {
    const { onClose } = open({
      "POST /api/account/password": {
        status: 400,
        body: { error: "that is not your current password" },
      },
    });

    await fill("wrong-horse", "a-brand-new-one");
    await userEvent.click(screen.getByRole("button", { name: "Change it" }));

    expect(
      await screen.findByText("that is not your current password"),
    ).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
