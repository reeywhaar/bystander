import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWith } from "@app/test/harness";

import { LoginPage } from "@app/apps/login/LoginPage";

const forgotten = { name: "Forgotten your password?" };

describe("LoginPage", () => {
  it("offers a way back in where this instance can mail one", async () => {
    renderWith(<LoginPage />, {
      "GET /api/instance": { body: { recovery: true } },
    });
    expect(await screen.findByRole("link", forgotten)).toBeInTheDocument();
  });

  /*
   * The gate, and the reason the instance is asked at all.
   *
   * Recovery by mail needs a relay. Offering the link anyway would send somebody who is
   * already locked out to a form that takes an address, says "check your inbox", and is
   * lying — and they would wait on it. Where there is no relay the only way back in is
   * whoever runs the instance handing over a link, so the form says nothing rather than
   * something untrue.
   */
  it("offers nothing where it cannot", async () => {
    renderWith(<LoginPage />, {
      "GET /api/instance": { body: { recovery: false } },
    });

    expect(await screen.findByLabelText("Name")).toBeInTheDocument();
    expect(screen.queryByRole("link", forgotten)).not.toBeInTheDocument();
  });

  // The form is what almost everybody came for, so it must not wait on that question —
  // and a refusal must not leave it holding a link to a page that will not work.
  it("still signs people in when the question cannot be answered", async () => {
    renderWith(<LoginPage />, {
      "GET /api/instance": { status: 500, body: { error: "no" } },
    });

    expect(await screen.findByLabelText("Name")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByRole("link", forgotten)).not.toBeInTheDocument();
    });
  });
});
