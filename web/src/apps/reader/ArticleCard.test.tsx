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
    image_width: 0,
    image_height: 0,
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
  columns: 1,
  rule: false,
  shot: 2,
  fit: "cover",
  aside: false,
  ...over,
});

/**
 * The aspect ratio a card's picture was actually drawn at, as width over height.
 *
 * Read as a number, because the value written is not the value stored: jsdom normalises
 * `aspectRatio: 1` to the string "1 / 1", and what these tests are about is the shape, not
 * how a browser chose to spell it.
 */
function drawnRatio(): number {
  const img = document.querySelector("img");
  if (!img) throw new Error("the card drew no picture");

  const [width, height = "1"] = img.style.aspectRatio.split("/");
  return Number(width) / Number(height);
}

describe("ArticleCard pictures", () => {
  const show = (width: number, height: number, slot: Slot = "standard") =>
    render(
      <ArticleCard
        article={article({ image_width: width, image_height: height, slot })}
        style={plain()}
        voice="didone"
        onRead={vi.fn()}
      />,
    );

  // The whole reason for measuring them. A photograph that is nearly square, cut to the
  // ladder's five-by-three, loses a third of itself — and nobody here has looked at it to
  // know whether the third mattered.
  it("draws a measured picture at its own shape", () => {
    show(1600, 1200);
    expect(drawnRatio()).toBeCloseTo(4 / 3);
  });

  it("believes a panorama and a portrait, inside the bounds", () => {
    // Wider than anything the drawn ladder can express, and drawn as it is.
    show(2000, 800);
    expect(drawnRatio()).toBeCloseTo(2.5);

    document.body.innerHTML = "";
    show(800, 2000);
    expect(drawnRatio()).toBeCloseTo(0.4);
  });

  // Squared, not clamped to the nearest bound, and this is the case that distinguishes them.
  // A 4:1 banner held at the widest allowed shape is still very nearly a 4:1 banner: it keeps
  // the letterbox proportions that made it wrong for a column of stories.
  it("squares a picture too wide for the page rather than nearly allowing it", () => {
    show(4000, 1000);
    expect(drawnRatio()).toBe(1);
  });

  it("squares a picture too tall for the page", () => {
    // Tall enough to push its own headline off the screen.
    show(1000, 4000);
    expect(drawnRatio()).toBe(1);
  });

  // Filled, so squaring takes the middle of the picture instead of letterboxing it into a
  // square hole — which is the shape the squaring was avoiding.
  it("fills a measured picture rather than fitting it", () => {
    show(4000, 1000);
    expect(document.querySelector("img")!.className).toContain("object-cover");
    expect(document.querySelector("img")!.className).not.toContain(
      "object-contain",
    );
  });

  // The ladder is what to do knowing nothing, and it stays that: a drawn shape from
  // five-by-three to square, in styles.css.
  it("leaves an unmeasured picture to the drawn ladder", () => {
    render(
      <ArticleCard
        article={article({ image_width: 0, image_height: 0 })}
        style={plain({ shot: 3 })}
        voice="didone"
        onRead={vi.fn()}
      />,
    );
    const img = document.querySelector("img")!;
    expect(img.style.aspectRatio).toBe("");
    expect(img.className).toContain("shot-3");
  });

  // A full-width picture at its own shape could be a screen-high photograph above the story
  // it belongs to. The lead's ratio is what stops the picture becoming the page.
  it("keeps the lead's own cinematic shape whatever it measures", () => {
    show(1000, 4000, "lead");
    const img = document.querySelector("img")!;
    expect(img.className).toContain("aspect-[21/9]");
  });
});

describe("ArticleCard layout", () => {
  const render1 = (style: Style, slot: Slot = "standard") =>
    render(
      <ArticleCard
        article={article({ image_width: 800, image_height: 600, slot })}
        style={style}
        voice="didone"
        onRead={vi.fn()}
      />,
    );

  const boxed = { line: "solid" as const, width: 1, ink: 1, pad: 1 };

  // A picture beside the story needs an edge to sit against, or it reads as one that failed
  // to be full width. The frame is that edge.
  it("sets a picture beside the story only on a boxed card", () => {
    render1(plain({ frame: boxed, aside: true }));
    expect(document.querySelector("article")!.className).toContain(
      "card-aside",
    );

    document.body.innerHTML = "";
    render1(plain({ frame: null, aside: true }));
    expect(document.querySelector("article")!.className).not.toContain(
      "card-aside",
    );
  });

  // The lead runs the width of the page and its picture opens the page. That is a different
  // job from illustrating a column.
  it("never sets the lead's picture beside it", () => {
    render1(plain({ frame: boxed, aside: true }), "lead");
    expect(document.querySelector("article")!.className).not.toContain(
      "card-aside",
    );
  });

  // The drawn number is a preference; the slot is the constraint.
  it("gives a card only as many columns as its width can carry", () => {
    render1(plain({ columns: 4 }), "lead");
    expect(document.querySelector(".prose-summary")!.className).toContain(
      "prose-columns-4",
    );

    document.body.innerHTML = "";
    render1(plain({ columns: 4 }), "feature");
    const feature = document.querySelector(".prose-summary")!.className;
    expect(feature).toContain("prose-columns-2");
    expect(feature).not.toContain("prose-columns-4");

    document.body.innerHTML = "";
    render1(plain({ columns: 4 }), "standard");
    expect(document.querySelector(".prose-summary")!.className).not.toContain(
      "prose-columns",
    );
  });

  // Two things competing for one measure is how a card ends up with neither.
  it("does not set a body in columns beside a picture", () => {
    render1(plain({ frame: boxed, aside: true, columns: 4 }), "wide");
    const card = document.querySelector("article")!;
    expect(card.className).toContain("card-aside");
    expect(document.querySelector(".prose-summary")!.className).not.toContain(
      "prose-columns",
    );
  });
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
