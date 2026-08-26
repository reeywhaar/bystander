import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderWith } from "@app/test/harness";

import { LandingPage } from "@app/apps/login/LandingPage";

function open() {
  return renderWith(<LandingPage />, {
    "POST /api/login": { status: 204 },
  });
}

describe("LandingPage", () => {
  // It argues rather than lists: the one thing worth saying about this reader is a claim
  // about what it refuses to do, so that is what it opens with.
  it("leads with the claim", () => {
    open();
    expect(
      screen.getByRole("heading", { name: /no unread count/i, level: 1 }),
    ).toBeInTheDocument();
  });

  // Two ways in from the same page, and they are the same dialog rather than two.
  it("offers a way in from the header and from the page", async () => {
    open();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByLabelText("Name")).toBeInTheDocument();
  });

  // Generated from the same run that makes the README's, and served from the bundle rather
  // than from docs/ — which nothing serves.
  it("shows the product rather than describing it", () => {
    open();
    const shots = screen
      .getAllByRole("img")
      .map((img) => img.getAttribute("src"));
    expect(shots).toEqual([
      "/landing/frontpage.webp",
      "/landing/feeds.webp",
      "/landing/feed.webp",
      "/landing/pages.webp",
      "/landing/page.webp",
      "/landing/read.webp",
    ]);
    for (const img of screen.getAllByRole("img")) {
      expect(img).toHaveAttribute("alt", expect.stringMatching(/\S/));
    }
  });

  // Whose it is, said once at the top as well as in the colophon at the bottom.
  it("names who made it under the claim", () => {
    open();
    const bylines = screen.getAllByRole("link", { name: "Misha Vyrtsev" });
    expect(bylines.length).toBeGreaterThan(0);
    for (const link of bylines) {
      expect(link).toHaveAttribute("href", "https://vyrtsev.com");
    }
  });

  it("says how somebody gets an account, since they cannot make one", () => {
    open();
    expect(screen.getByText(/invitation-only/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /on GitHub/ })).toHaveAttribute(
      "href",
      "https://github.com/reeywhaar/bystander",
    );
  });
});
