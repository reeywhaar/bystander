import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Session } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { SessionsDialog } from "@app/apps/manage/SessionsDialog";

const NOW = Math.floor(Date.now() / 1000);

function session(overrides: Partial<Session> = {}): Session {
  return {
    id: "se_one",
    current: false,
    created_at: NOW - 3 * 24 * 3600,
    last_access: NOW - 2 * 3600,
    expires_at: NOW + 4 * 24 * 3600,
    ip: "198.51.100.9",
    user_agent: "Mozilla/5.0 (Macintosh) Safari/605.1.15",
    device: "Safari on Mac",
    ...overrides,
  };
}

/** How many times a "METHOD /path" was asked for. */
function count(
  transport: { calls: { method: string; path: string }[] },
  method: string,
  path: string,
) {
  return transport.calls.filter((c) => c.method === method && c.path === path)
    .length;
}

function open(sessions: Session[], onClose = () => {}) {
  return renderWith(<SessionsDialog open onClose={onClose} />, {
    "GET /api/account/sessions": { body: sessions },
    "DELETE /api/account/sessions": { status: 204 },
    "DELETE /api/account/sessions/se_one": { status: 204 },
    "DELETE /api/account/sessions/se_two": { status: 204 },
  });
}

describe("SessionsDialog", () => {
  // The summary is a guess at a string built out of thirty years of compatibility lies, so
  // the string it was guessed from stays on the row. Recognising a session is the whole job.
  it("shows the browser, the address and the raw agent", async () => {
    open([session()]);

    expect(await screen.findByText("Safari on Mac")).toBeInTheDocument();
    expect(screen.getByText(/198\.51\.100\.9/)).toBeInTheDocument();
    expect(
      screen.getByText("Mozilla/5.0 (Macintosh) Safari/605.1.15"),
    ).toBeInTheDocument();
  });

  // A bare relative time in a row that already carries two other dates is one among three.
  it("labels the last access", async () => {
    open([session({ last_access: NOW - 2 * 3600 })]);

    expect(await screen.findByText("Last access")).toBeInTheDocument();
    expect(screen.getByText("2 hours ago")).toBeInTheDocument();
  });

  it("says which session is the one reading", async () => {
    open([session({ id: "se_one", current: true }), session({ id: "se_two" })]);

    expect(await screen.findByText("this one")).toBeInTheDocument();
  });

  // Ending a session is not undoable and the button is one of several in a list.
  it("asks before ending a session", async () => {
    const { transport } = open([session({ id: "se_two" })]);
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    expect(count(transport, "DELETE", "/api/account/sessions/se_two")).toBe(0);

    await user.click(screen.getByRole("button", { name: "Sign it out" }));
    await waitFor(() =>
      expect(count(transport, "DELETE", "/api/account/sessions/se_two")).toBe(
        1,
      ),
    );
  });

  it("lets a second press be taken back", async () => {
    const { transport } = open([session({ id: "se_two" })]);
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await user.click(screen.getByRole("button", { name: "Keep" }));

    expect(
      screen.getByRole("button", { name: "Sign out" }),
    ).toBeInTheDocument();
    expect(count(transport, "DELETE", "/api/account/sessions/se_two")).toBe(0);
  });

  // "Sign out everywhere else" over a list of one would end nothing, so it is absent rather
  // than offered and inert.
  it("offers to sign the others out only when there are others", async () => {
    open([session({ current: true })]);

    await screen.findByText("this one");
    expect(screen.queryByText(/Sign out the other/)).not.toBeInTheDocument();
  });

  it("counts the others in the button that ends them", async () => {
    const { transport } = open([
      session({ id: "se_one", current: true }),
      session({ id: "se_two" }),
      session({ id: "se_three" }),
    ]);
    const user = userEvent.setup();

    await user.click(
      await screen.findByRole("button", { name: "Sign out the other 2" }),
    );
    await waitFor(() =>
      expect(count(transport, "DELETE", "/api/account/sessions")).toBe(1),
    );
  });

  // Revoking the session you are reading from is a coherent thing to want, and the answer is
  // that you land somewhere else — a whole-document navigation, because "/" is served
  // differently with a cookie and without and only the server can tell which this now is.
  it("leaves the page when the current session is the one ended", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });

    open([session({ id: "se_one", current: true })]);
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await user.click(screen.getByRole("button", { name: "Sign out here" }));

    await waitFor(() => expect(assign).toHaveBeenCalledWith("/"));
    vi.unstubAllGlobals();
  });

  // Nothing is recorded to answer a question nobody asked: the list is fetched only while
  // its dialog is open, and opening it is what somebody with a reason does.
  it("asks for nothing while it is closed", () => {
    const { transport } = renderWith(
      <SessionsDialog open={false} onClose={() => {}} />,
      { "GET /api/account/sessions": { body: [] } },
    );

    expect(count(transport, "GET", "/api/account/sessions")).toBe(0);
  });
});
