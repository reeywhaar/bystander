import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Article, Slot } from "@app/api/types";

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

describe("ArticleCard", () => {
  it("carries the slot the server chose", () => {
    for (const slot of ["lead", "feature", "standard", "brief"] as Slot[]) {
      const { container, unmount } = render(
        <ArticleCard article={article({ slot })} onRead={() => {}} />,
      );
      expect(container.querySelector("article")).toHaveClass(`slot-${slot}`);
      unmount();
    }
  });

  it("marks read when the headline is opened", async () => {
    const onRead = vi.fn();
    render(<ArticleCard article={article()} onRead={onRead} />);

    await userEvent.click(screen.getByRole("link", { name: "A headline" }));
    expect(onRead).toHaveBeenCalledWith("a_1", true);
  });

  it("toggles back", async () => {
    const onRead = vi.fn();
    render(
      <ArticleCard
        article={article({ read_at: 1_787_000_100 })}
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
      <ArticleCard article={article({ slot: "brief" })} onRead={() => {}} />,
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
        onRead={() => {}}
      />,
    );
    expect(screen.getByText("The Go Blog")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "The Go Blog" }),
    ).not.toBeInTheDocument();
  });

  it("opens articles in a new tab, without handing the opener over", () => {
    render(<ArticleCard article={article()} onRead={() => {}} />);
    const link = screen.getByRole("link", { name: "A headline" });
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });
});
