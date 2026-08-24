import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Modal } from "@app/components/ui/Modal";

function open(onClose = vi.fn()) {
  const { container } = render(
    <Modal open onClose={onClose} title="A dialog">
      <p>inside</p>
    </Modal>,
  );
  const dialog = container.querySelector("dialog");
  if (!dialog) throw new Error("no dialog rendered");
  return { dialog, onClose };
}

describe("Modal", () => {
  it("shows its contents", () => {
    open();
    expect(screen.getByText("A dialog")).toBeInTheDocument();
    expect(screen.getByText("inside")).toBeInTheDocument();
  });

  // A click on the backdrop targets the <dialog> element itself.
  it("closes when the backdrop is clicked", () => {
    const { dialog, onClose } = open();

    fireEvent.pointerDown(dialog);
    fireEvent.click(dialog);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("stays open when something inside is clicked", () => {
    const { onClose } = open();

    const inside = screen.getByText("inside");
    fireEvent.pointerDown(inside);
    fireEvent.click(inside);
    expect(onClose).not.toHaveBeenCalled();
  });

  // Selecting a URL and dragging past the edge of the dialog produces a click whose target
  // is the dialog. Closing on that would throw away what somebody was in the middle of.
  it("stays open when a drag starts inside and ends on the backdrop", () => {
    const { dialog, onClose } = open();

    fireEvent.pointerDown(screen.getByText("inside"));
    fireEvent.click(dialog);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on escape", () => {
    const { dialog, onClose } = open();

    fireEvent(
      dialog,
      new Event("cancel", { bubbles: false, cancelable: true }),
    );
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  /*
   * A modal <dialog> does not stop the page behind it scrolling. showModal() makes the rest
   * of the document inert, which is about focus and pointer targeting rather than the wheel:
   * measured in Chromium, a wheel over the backdrop scrolled the page underneath by the full
   * delta, and so did a wheel inside a dialog that had reached the end of its own scroll.
   */
  describe("holding what is behind it still", () => {
    it("stops the page scrolling while it is open", () => {
      const { rerender } = render(
        <Modal open={false} onClose={vi.fn()} title="A dialog">
          <p>inside</p>
        </Modal>,
      );
      expect(document.body.style.overflow).toBe("");

      rerender(
        <Modal open onClose={vi.fn()} title="A dialog">
          <p>inside</p>
        </Modal>,
      );
      expect(document.body.style.overflow).toBe("hidden");

      rerender(
        <Modal open={false} onClose={vi.fn()} title="A dialog">
          <p>inside</p>
        </Modal>,
      );
      expect(document.body.style.overflow).toBe("");
    });

    // The page must not get its scroll back while something is still on top of it.
    it("keeps the page still until the last dialog closes", () => {
      const both = (inner: boolean) => (
        <Modal open onClose={vi.fn()} title="Outer">
          <p>outer</p>
          <Modal open={inner} onClose={vi.fn()} title="Inner">
            <p>inner</p>
          </Modal>
        </Modal>
      );

      const { rerender } = render(both(true));
      expect(document.body.style.overflow).toBe("hidden");

      // The inner one closes; the outer one is still up.
      rerender(both(false));
      expect(document.body.style.overflow).toBe("hidden");
    });

    /*
     * The half that holding the page still does not cover. A dialog opened from inside
     * another leaves the first one scrollable — inert to clicks, not to scrolling — so what
     * somebody was reading slides away behind what they just opened. Measured in Chromium
     * with only the page held: a wheel over the parent moved it 894px.
     */
    it("stops the dialog it was opened from scrolling", () => {
      const { container, rerender } = render(
        <Modal open onClose={vi.fn()} title="Outer">
          <p>outer</p>
          <Modal open={false} onClose={vi.fn()} title="Inner">
            <p>inner</p>
          </Modal>
        </Modal>,
      );
      const outer = container.querySelector("dialog");
      if (!outer) throw new Error("no dialog rendered");
      expect(outer.style.overflow).toBe("");

      rerender(
        <Modal open onClose={vi.fn()} title="Outer">
          <p>outer</p>
          <Modal open onClose={vi.fn()} title="Inner">
            <p>inner</p>
          </Modal>
        </Modal>,
      );
      expect(outer.style.overflow).toBe("hidden");

      rerender(
        <Modal open onClose={vi.fn()} title="Outer">
          <p>outer</p>
          <Modal open={false} onClose={vi.fn()} title="Inner">
            <p>inner</p>
          </Modal>
        </Modal>,
      );
      expect(outer.style.overflow).toBe("");
    });

    // Unmounting while open is a route change, or a parent that stopped rendering it. The
    // page would be left locked with nothing on screen to explain why.
    it("gives the page back when it is unmounted while open", () => {
      const { unmount } = render(
        <Modal open onClose={vi.fn()} title="A dialog">
          <p>inside</p>
        </Modal>,
      );
      expect(document.body.style.overflow).toBe("hidden");

      unmount();
      expect(document.body.style.overflow).toBe("");
    });
  });
});
