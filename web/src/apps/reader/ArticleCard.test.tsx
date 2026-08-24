import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Article, Slot } from "@app/api/types";

import type { Style } from "@app/lib/voice";

import { ArticleCard } from "@app/apps/reader/ArticleCard";

function article(overrides: Partial<Article> = {}): Article {
  return {
    id: "a_1",
    rank: 0,
    slot: "standard",
    read_at: null,
    title: "A headline",
    link: "https://example.com/story",
    author: "",
    summary: "<p>A standfirst</p>",
    image_url: "https://example.com/pic.png",
    published_at: 1_787_000_000,
    feed: { id: "f_1", title: "The Example", site_url: "https://example.com" },
    ...overrides,
  };
}

/**
 * A non-primary mouse click, as a browser dispatches it.
 *
 * Not `fireEvent.auxClick`: Testing Library's typed event map does not carry auxclick, and
 * this is closer to the thing being tested anyway — the event React's `onAuxClick` is
 * bound to. `bubbles` is load-bearing, because React attaches its listener at the root
 * rather than to the element.
 */
function auxClick(element: Element, button: number) {
  fireEvent(
    element,
    new MouseEvent("auxclick", { button, bubbles: true, cancelable: true }),
  );
}

/** A plain style, so a test that is not about presentation need not describe one. */
const plain = (over: Partial<Style> = {}): Style => ({
  voice: "didone",
  prose: 1,
  frame: null,
  columns: false,
  rule: false,
  shot: 2,
  fit: "cover",
  ...over,
});

describe("ArticleCard", () => {
  // The size is the story's, from a ladder in styles.css, and the slot only sets a floor
  // under it. A utility here would win the cascade and flatten both.
  it("sets its standfirst from the ladder, not from a size utility", () => {
    const { container } = render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="gothic"
        onRead={() => {}}
      />,
    );
    const summary = container.querySelector(".prose-summary");
    expect(summary?.className).toMatch(/\bprose-step-[0-3]\b/);
    expect(summary?.className).not.toMatch(/\btext-(xs|sm|base|lg|[0-9]xl)\b/);
  });

  it("carries the voice the page gave it, and no size of its own", () => {
    const { container } = render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="gothic"
        onRead={() => {}}
      />,
    );
    const headline = container.querySelector("h2");
    expect(headline).toHaveClass("headline");
    expect(headline).toHaveClass("voice-gothic");
    // The size belongs to the slot and the voice together, in styles.css. A Tailwind size
    // utility here would win the cascade and take the voice's own scale with it.
    expect(headline?.className).not.toMatch(/\btext-(xs|sm|base|lg|[0-9]xl)\b/);
  });

  it("carries the slot the server chose", () => {
    for (const slot of ["lead", "feature", "standard", "brief"] as Slot[]) {
      const { container, unmount } = render(
        <ArticleCard
          article={article({ slot })}
          style={plain()}
          voice="didone"
          onRead={() => {}}
        />,
      );
      expect(container.querySelector("article")).toHaveClass(`slot-${slot}`);
      unmount();
    }
  });

  it("marks read when the headline is opened", async () => {
    const onRead = vi.fn();
    render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="didone"
        onRead={onRead}
      />,
    );

    await userEvent.click(screen.getByRole("link", { name: "A headline" }));
    expect(onRead).toHaveBeenCalledWith("a_1", true);
  });

  // Middle click is how somebody opens a stack of articles in background tabs — the exact
  // gesture a front page invites. Browsers dispatch `auxclick` for it and React's onClick
  // maps only to `click`, so it silently did not count as opening anything.
  it("marks read when the headline is opened with the middle button", () => {
    const onRead = vi.fn();
    render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="didone"
        onRead={onRead}
      />,
    );

    auxClick(screen.getByRole("link", { name: "A headline" }), 1);
    expect(onRead).toHaveBeenCalledWith("a_1", true);
  });

  it("marks read when the picture is opened with the middle button", () => {
    const onRead = vi.fn();
    const { container } = render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="didone"
        onRead={onRead}
      />,
    );

    // The picture is a second link to the same article, hidden from the accessibility
    // tree because it says nothing the headline does not.
    const picture = container.querySelector('a[aria-hidden="true"]');
    expect(picture).not.toBeNull();

    auxClick(picture!, 1);
    expect(onRead).toHaveBeenCalledWith("a_1", true);
  });

  // auxclick fires for the right button too, and raising a context menu is not reading.
  it("does not mark read when the context menu is raised", () => {
    const onRead = vi.fn();
    render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="didone"
        onRead={onRead}
      />,
    );

    auxClick(screen.getByRole("link", { name: "A headline" }), 2);
    expect(onRead).not.toHaveBeenCalled();
  });

  it("toggles back", async () => {
    const onRead = vi.fn();
    render(
      <ArticleCard
        article={article({ read_at: 1_787_000_100 })}
        style={plain()}
        voice="didone"
        onRead={onRead}
      />,
    );

    await userEvent.click(screen.getByRole("button", { name: "Mark unread" }));
    expect(onRead).toHaveBeenCalledWith("a_1", false);
  });

  // A read card recedes without moving. Where an article sits is how somebody remembers
  // where they were, so nothing may reorder or remove it.
  it("greys a read article in place", () => {
    const { container } = render(
      <ArticleCard
        article={article({ read_at: 1_787_000_100 })}
        style={plain()}
        voice="didone"
        onRead={() => {}}
      />,
    );
    expect(container.querySelector("article")).toHaveClass("is-read");
    expect(
      screen.getByRole("link", { name: "A headline" }),
    ).toBeInTheDocument();
  });

  it("renders the server's sanitized summary as markup", () => {
    render(
      <ArticleCard
        article={article({
          summary: '<p>Read <a href="https://example.com/x">this</a></p>',
        })}
        style={plain()}
        voice="didone"
        onRead={() => {}}
      />,
    );
    // The link inside the standfirst survives, which is the whole reason the summary is
    // markup rather than stripped text.
    expect(screen.getByRole("link", { name: "this" })).toHaveAttribute(
      "href",
      "https://example.com/x",
    );
  });

  // A card sized for a picture that has no picture is what makes a page look broken.
  it("shows neither picture nor standfirst on a brief", () => {
    const { container } = render(
      <ArticleCard
        article={article({ slot: "brief" })}
        style={plain()}
        voice="didone"
        onRead={() => {}}
      />,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(screen.queryByText("A standfirst")).not.toBeInTheDocument();
  });

  // A feed that declares no site link — the Go Blog's is exactly that — must not get a
  // link that downloads an XML file.
  it("names a source without a site link, without linking it", () => {
    render(
      <ArticleCard
        article={article({
          feed: { id: "f_1", title: "The Go Blog", site_url: "" },
        })}
        style={plain()}
        voice="didone"
        onRead={() => {}}
      />,
    );
    expect(screen.getByText("The Go Blog")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "The Go Blog" }),
    ).not.toBeInTheDocument();
  });

  it("opens articles in a new tab, without handing the opener over", () => {
    render(
      <ArticleCard
        article={article()}
        style={plain()}
        voice="didone"
        onRead={() => {}}
      />,
    );
    const link = screen.getByRole("link", { name: "A headline" });
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });
});
