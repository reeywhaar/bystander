import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Boundary } from "@app/components/Boundary";

function Throws(): never {
  throw new Error("the feeds could not be read");
}

describe("Boundary", () => {
  // React logs the caught error itself, and a test that prints a stack it expected is a test
  // whose output nobody reads.
  beforeEach(() => vi.spyOn(console, "error").mockImplementation(() => {}));
  afterEach(() => vi.restoreAllMocks());

  it("says what broke rather than going blank", () => {
    render(
      <Boundary>
        <Throws />
      </Boundary>,
    );
    expect(screen.getByText("the feeds could not be read")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Try again" }),
    ).toBeInTheDocument();
  });

  // A boundary replaces the whole island, so without this the document says nothing anywhere
  // about what it is — and somebody looking at a broken instance is somebody who may be
  // trying to find that out, or find somewhere to report it.
  it("still says what this is and who made it", () => {
    render(
      <Boundary>
        <Throws />
      </Boundary>,
    );
    expect(screen.getByRole("link", { name: "Bystander" })).toHaveAttribute(
      "href",
      "https://github.com/reeywhaar/bystander",
    );
    expect(screen.getByRole("link", { name: "Misha Vyrtsev" })).toHaveAttribute(
      "href",
      "https://vyrtsev.com",
    );
  });

  it("gets out of the way when nothing threw", () => {
    render(
      <Boundary>
        <p>the page</p>
      </Boundary>,
    );
    expect(screen.getByText("the page")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Bystander" }),
    ).not.toBeInTheDocument();
  });
});
