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
    expect(screen.getByRole("link", { name: /is on GitHub/ })).toHaveAttribute(
      "href",
      "https://github.com/reeywhaar/bystander",
    );
  });

  /*
   * This instance is somebody else's and invitation-only, so for nearly everybody reading
   * this the one thing they can actually do is take the whole of it and run their own. A
   * page whose only clear action is a sign-in nobody can use has no clear action at all.
   */
  it("offers the source as the action, not as a footnote", () => {
    open();

    const cta = screen.getByRole("link", { name: "GitHub" });
    expect(cta).toHaveAttribute(
      "href",
      "https://github.com/reeywhaar/bystander",
    );
    // It leaves for somewhere else, so it is a link the browser can open in its own tab
    // rather than a button that navigates.
    expect(cta).toHaveAttribute("target", "_blank");
    expect(cta).toHaveAttribute("rel", expect.stringContaining("noopener"));

    // And what it is, said beside the action rather than at the bottom of the page.
    expect(
      screen.getByText(/Self-hosted, and free to run/),
    ).toBeInTheDocument();
  });

  // In the top as well, where somebody convinced by the first screen goes looking for it.
  it("carries the source in the masthead", () => {
    open();
    expect(screen.getByRole("link", { name: "Source" })).toHaveAttribute(
      "href",
      "https://github.com/reeywhaar/bystander",
    );
  });

  /*
   * The page argues that this reader sets a front page in six display faces. A document
   * making that argument with every heading in one serif is arguing against itself.
   */
  it("sets its headings in the faces it is arguing for", () => {
    open();

    const voices = screen
      .getAllByRole("heading", { level: 2 })
      .map((heading) =>
        [...heading.classList].find((name) => name.startsWith("voice-")),
      );

    expect(voices).toEqual([
      "voice-antique",
      "voice-slab",
      "voice-gothic",
      "voice-humanist",
    ]);
    // Sized through the variable rather than a `text-*` utility, which would win the cascade
    // and take the voice's own scale out with it — leaving six faces at one size looking
    // like six sizes.
    for (const heading of screen.getAllByRole("heading", { level: 2 })) {
      expect(heading).toHaveClass("heading-voiced");
    }
  });
});
