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
});
