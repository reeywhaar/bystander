import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it } from "vitest";

import type { Recovery } from "@app/api/types";
import { renderWith, type Recording } from "@app/test/harness";

import { RecoverPage } from "@app/apps/login/RecoverPage";

const TOKEN = "abc123";

function link(overrides: Partial<Recovery> = {}): Recovery {
  return {
    username: "alice",
    // A fortnight out, so "expired" is only ever true because a test said so.
    expires_at: Math.floor(Date.now() / 1000) + 1209600,
    usable: true,
    used: false,
    voided: false,
    expired: false,
    ...overrides,
  };
}

function open(recording: Recording, path = `/recover/${TOKEN}`) {
  return renderWith(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/recover/:token" element={<RecoverPage />} />
        <Route path="/recover" element={<RecoverPage />} />
      </Routes>
    </MemoryRouter>,
    recording,
  );
}

function state(body: Recovery): Recording {
  return { [`GET /api/recoveries/${TOKEN}`]: { body } };
}

describe("RecoverPage", () => {
  // The name is the only thing that makes a link checkable by the person holding it.
  it("says whose account the link opens", async () => {
    open(state(link()));
    expect(await screen.findByText("alice")).toBeInTheDocument();
  });

  it("sets the password and sends them to the login form", async () => {
    const { transport } = open({
      ...state(link()),
      [`POST /api/recoveries/${TOKEN}/accept`]: { status: 204 },
    });

    await userEvent.type(
      await screen.findByLabelText("New password"),
      "battery-staple",
    );
    await userEvent.type(screen.getByLabelText("Again"), "battery-staple");
    await userEvent.click(
      screen.getByRole("button", { name: "Set my password" }),
    );

    expect(
      await screen.findByRole("link", { name: "Sign in" }),
    ).toBeInTheDocument();
    const sent = transport.calls.find((c) => c.method === "POST");
    expect(sent?.body).toEqual({ password: "battery-staple" });
  });

  // The repeat exists to catch a typo in a password nobody can see, which is a question
  // about this form — so it is answered here, without a request.
  it("refuses two that do not match, without asking the server", async () => {
    const { transport } = open(state(link()));

    await userEvent.type(
      await screen.findByLabelText("New password"),
      "battery-staple",
    );
    await userEvent.type(screen.getByLabelText("Again"), "battery-stapel");
    await userEvent.click(
      screen.getByRole("button", { name: "Set my password" }),
    );

    expect(
      await screen.findByText("those two do not match"),
    ).toBeInTheDocument();
    expect(transport.calls.some((c) => c.method === "POST")).toBe(false);
  });

  // Four states a person acts on differently, which is the whole reason the page reads the
  // link before showing a form.
  it.each([
    [
      "used",
      link({ usable: false, used: true }),
      "That link has already been used",
    ],
    ["voided", link({ usable: false, voided: true }), "That link was replaced"],
    [
      "expired",
      link({ usable: false, expired: true }),
      "That link has expired",
    ],
  ])("offers no form when the link is %s", async (_name, body, heading) => {
    open(state(body));

    expect(await screen.findByText(heading)).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
    });
  });

  // Mail clients wrap long URLs, so half a link is a real thing to arrive holding.
  it("says so when the link was cut short", async () => {
    open({}, "/recover");
    expect(
      await screen.findByText("That link looks incomplete"),
    ).toBeInTheDocument();
  });
});
