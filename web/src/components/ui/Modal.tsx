import { useEffect, useRef, type ReactNode } from "react";

/**
 * A modal dialog, on the native `<dialog>` element.
 *
 * Native rather than a div with `role="dialog"`: `showModal()` brings focus trapping, Esc,
 * inertness of the rest of the page and the top layer with it, all of which are tedious to
 * reimplement and easy to reimplement slightly wrong.
 *
 * `showModal` is guarded because jsdom does not implement it — a test rendering this should
 * see the contents rather than a crash.
 */
export function Modal({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;

    if (open && !dialog.open) {
      if (typeof dialog.showModal === "function") dialog.showModal();
      else dialog.setAttribute("open", "");
    } else if (!open && dialog.open) {
      dialog.close();
    }
  }, [open]);

  return (
    <dialog
      ref={ref}
      onClose={onClose}
      // Esc fires `cancel` before `close`. Preventing the default and closing through the
      // same path means there is one way out, not two that can drift apart.
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      className="w-[min(28rem,calc(100vw-2rem))] rounded-md border border-rule bg-paper-raised
        p-0 text-ink backdrop:bg-black/50"
    >
      {open ? (
        <div className="flex flex-col gap-4 p-5">
          <h2 className="font-serif text-xl text-ink">{title}</h2>
          {children}
        </div>
      ) : null}
    </dialog>
  );
}
