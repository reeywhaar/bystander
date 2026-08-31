import { render } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useMasonry } from "@app/lib/masonry";

/** The callbacks of every ResizeObserver the component made, so a test can fire them. */
let observers: {
  cb: ResizeObserverCallback;
  observed: Element[];
  self: ResizeObserver;
}[] = [];

function stubResizeObserver() {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observed: Element[] = [];
      constructor(readonly cb: ResizeObserverCallback) {
        observers.push({
          cb,
          observed: this.observed,
          self: this as unknown as ResizeObserver,
        });
      }
      observe(el: Element) {
        this.observed.push(el);
      }
      unobserve() {}
      disconnect() {}
    },
  );
}

/** Gives each element a height that a test can change, the way a font swap would. */
function heights(el: HTMLElement, value: () => number) {
  el.getBoundingClientRect = () => ({ height: value(), width: 300 }) as DOMRect;
}

function Grid() {
  const grid = useRef<HTMLDivElement>(null);
  useMasonry(grid, []);
  return (
    <div ref={grid} data-testid="grid">
      <div data-testid="card" />
    </div>
  );
}

afterEach(() => {
  observers = [];
  vi.unstubAllGlobals();
});

describe("useMasonry", () => {
  // The bug: it re-packed on resize and on document.fonts.ready, so anything that changed a
  // card's height afterwards left its span stale. Too small and the card drew over the one
  // below it; too large and it left a gap the size of the difference. Both were reported, in
  // Safari and in Firefox, and they are the same fault in opposite directions.
  it("re-packs when a card's height changes under it", () => {
    stubResizeObserver();
    let height = 100;

    const { getByTestId } = render(<Grid />);
    const grid = getByTestId("grid");
    const card = getByTestId("card");
    heights(grid, () => 300);
    heights(card, () => height);

    // Pack once at the height it was first laid out at.
    const observer = observers[0];
    expect(observer).toBeDefined();
    observer!.cb(
      [{ target: card, contentRect: { height, width: 300 } }] as never,
      observer!.self,
    );
    const before = card.style.gridRowEnd;
    expect(before).toMatch(/^span \d+$/);

    // Now the face arrives and the headline loses a line.
    height = 60;
    observer!.cb(
      [{ target: card, contentRect: { height, width: 300 } }] as never,
      observer!.self,
    );

    expect(card.style.gridRowEnd).not.toBe(before);
  });

  // The observer fires once when it starts observing, and the grid's own height changes every
  // time we pack. Answering either would be answering ourselves.
  it("ignores the grid's height so packing cannot trigger itself", () => {
    stubResizeObserver();

    const { getByTestId } = render(<Grid />);
    const grid = getByTestId("grid");
    const card = getByTestId("card");
    heights(grid, () => 300);
    heights(card, () => 100);

    const observer = observers[0]!;
    // Settle at a known span.
    observer.cb(
      [{ target: card, contentRect: { height: 100, width: 300 } }] as never,
      observer.self,
    );
    const settled = card.style.gridRowEnd;

    // The grid got taller because we packed, and no wider. Width 0 because that is what
    // jsdom measured when the effect captured it, so only the height differs here.
    const packs = vi.spyOn(card.style, "gridRowEnd", "set");
    observer.cb(
      [{ target: grid, contentRect: { height: 9999, width: 0 } }] as never,
      observer.self,
    );
    expect(packs).not.toHaveBeenCalled();
    expect(card.style.gridRowEnd).toBe(settled);
  });

  // Every card, not only the grid — the grid's height changing is what packing does, and a
  // card's height changing is what packing is for.
  it("watches the grid and every card in it", () => {
    stubResizeObserver();

    const { getByTestId } = render(<Grid />);
    const observer = observers[0]!;

    expect(observer.observed).toContain(getByTestId("grid"));
    expect(observer.observed).toContain(getByTestId("card"));
  });
});
