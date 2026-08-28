import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Subscription } from "@app/api/types";

import { FeedErrorDialog } from "@app/apps/manage/FeedErrorDialog";

function feed(overrides: Partial<Subscription> = {}): Subscription {
  return {
    id: "s_1",
    feed_id: "f_1",
    url: "https://example.com/rss",
    site_url: "https://example.com",
    title: "The Example",
    feed_title: "The Example",
    title_override: "",
    note: "",
    priority: 50,
    tag_ids: [],
    article_window: 604800,
    created_at: 0,
    last_success_at: null,
    last_status: 0,
    last_error: "",
    last_error_body: "",
    failure_count: 1,
    ...overrides,
  };
}

const show = (overrides: Partial<Subscription>) =>
  render(<FeedErrorDialog feed={feed(overrides)} open onClose={vi.fn()} />);

describe("FeedErrorDialog", () => {
  // The two situations need telling apart before anything else is said, and only the status
  // separates them: a request that never arrived has no answer to quote.
  it("says the server refused, and quotes it", () => {
    show({
      last_status: 429,
      last_error: "the server answered 429 Too Many Requests",
      last_error_body: '{"error":"rate limited"}',
    });

    expect(screen.getByText(/answered, and refused/)).toBeInTheDocument();
    expect(screen.getByText("429")).toBeInTheDocument();
    expect(
      screen.getByText("the server answered 429 Too Many Requests"),
    ).toBeInTheDocument();
    expect(screen.getByText('{"error":"rate limited"}')).toBeInTheDocument();
  });

  it("says nothing answered, and quotes nothing", () => {
    show({
      last_status: 0,
      last_error: "could not reach https://example.com/rss: no such host",
      last_error_body: "",
    });

    expect(screen.getByText(/Nothing answered/)).toBeInTheDocument();
    // No empty box under a heading: that reads as something missing rather than as nothing
    // said.
    expect(screen.queryByText("What the server said")).toBeNull();
    expect(
      screen.getByText(/could not reach https:\/\/example.com\/rss/),
    ).toBeInTheDocument();
  });

  it("counts the failures and says when it last worked", () => {
    show({ failure_count: 7, last_success_at: 1 });
    expect(screen.getByText(/failed 7 times in a row/)).toBeInTheDocument();

    document.body.innerHTML = "";
    show({ failure_count: 1, last_success_at: null });
    expect(screen.getByText(/first failure/)).toBeInTheDocument();
    expect(screen.getByText(/never worked/)).toBeInTheDocument();
  });
});
