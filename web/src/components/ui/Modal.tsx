import {
  createContext,
  useContext,
  useEffect,
  useRef,
  type ReactNode,
  type RefObject,
} from "react";

import { lockScroll } from "@app/components/ui/scrollLock";

/**
 * The dialog a piece of the tree is inside, if it is inside one.
 *
 * Only so that a dialog opened from within another dialog can hold that one still — see the
 * effect below. A ref rather than the element, because the provider is the dialog itself and
 * its element does not exist until after the first render; by the time a child's effect runs
 * and reads `.current`, it does.
 */
const DialogContext = createContext<RefObject<HTMLDialogElement | null> | null>(
  null,
);

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
  footer,
  wide = false,
  flush = false,
  onPaper = false,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /**
   * Set on the reading paper rather than the raised white.
   *
   * For a dialog showing what a page would show. Everything the reader reads is on `paper`;
   * `paper-raised` is the surface things sit *on top of* it on — dialogs, cards, inputs. A
   * dialog whose job is to be a sample of the page belongs on the same stock as the page, or
   * it is showing the articles against a background they will never appear against.
   *
   * The footer stays raised either way, which is what turns the difference into a division:
   * the part you read, and the bar you press.
   */
  onPaper?: boolean;

  /**
   * The footer's rule sits directly on the body, with no space above it.
   *
   * For a body that scrolls to the footer. The space this removes is the container's own gap,
   * and between a cut-off line of text and the rule it reads as a mistake rather than as
   * breathing room — the content stops, then there is a blank strip, then the rule. A scroll
   * region should run right up to its edge, and the room after the last item belongs inside
   * it, where scrolling to the end reveals it.
   */
  flush?: boolean;

  /**
   * A dialog for reading rather than for answering.
   *
   * Twenty-eight rem is a form: a few fields, a list of names, a question. Prose needs more
   * than that — a feed's articles set to a form's width are a column of forty characters,
   * which is the shape this width exists to avoid everywhere else on the page.
   */
  wide?: boolean;
  /**
   * What this dialog does, as buttons.
   *
   * Laid out here rather than at each call site, because six dialogs laying out their own
   * button row produced six arrangements — primary left, primary right, one pair pushed
   * apart — and which button is Save became something to look for rather than something
   * to know. The order is: whatever dismisses, then the one that acts, along the right.
   * An action that belongs to neither group — a destructive one, an alternative export —
   * takes `mr-auto` and sits on the left.
   *
   */
  footer?: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const parent = useContext(DialogContext);
  // Where the press started. A click on the backdrop targets the <dialog> itself, but so
  // does one that began on the text inside and finished outside it — which is what
  // selecting a URL and dragging past the edge looks like. Requiring both ends to be on
  // the backdrop is the difference between closing on purpose and closing by accident.
  const startedOnBackdrop = useRef(false);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;

    if (open && !dialog.open) {
      if (typeof dialog.showModal === "function") dialog.showModal();
      else dialog.setAttribute("open", "");
    } else if (!open && dialog.open) {
      // Guarded the same way, and for the same reason. Only `showModal` was, which held for
      // as long as nothing closed a dialog by toggling `open` — the attribute fallback opens
      // it and then there is no `close` to undo that.
      if (typeof dialog.close === "function") dialog.close();
      else dialog.removeAttribute("open");
    }
  }, [open]);

  /**
   * Nothing behind this scrolls while it is open — not the page, and not the dialog this one
   * was opened from.
   *
   * Both are needed and neither covers the other. Holding the page still leaves a parent
   * dialog free to scroll away underneath; holding the parent still leaves the page free to
   * scroll behind both. Measured in Chromium: with only the page held, a wheel over the
   * parent moved it eight hundred pixels.
   *
   * Separate from the effect that opens the dialog, because this one has to undo itself. An
   * unmount while open — a route change, a parent deciding to stop rendering it — would
   * otherwise leave the page locked with nothing on screen to explain why.
   */
  useEffect(() => {
    if (!open) return;

    const releasePage = lockScroll(document.body);
    const above = parent?.current;
    const releaseParent = above ? lockScroll(above) : undefined;

    return () => {
      releaseParent?.();
      releasePage();
    };
  }, [open, parent]);

  /*
    How much room sits under the body: the dialog's own padding when nothing follows it, the
    same gap the body's blocks get when a footer does, and none at all when the footer's rule
    is meant to sit on a scroll edge.
  */
  const bodyBottom = !footer ? "pb-5" : flush ? "pb-0" : "pb-4";

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
      onPointerDown={(event) => {
        startedOnBackdrop.current = event.target === event.currentTarget;
      }}
      onClick={(event) => {
        if (startedOnBackdrop.current && event.target === event.currentTarget)
          onClose();
        startedOnBackdrop.current = false;
      }}
      // `m-auto` is load-bearing. A modal <dialog> is centred by the user agent with
      // `inset: 0; margin: auto`, and Tailwind's preflight resets `margin: 0` on every
      // element — which leaves only the inset, and drops the dialog in the top-left corner.
      //
      // `max-h` and the scroll are for the picker: a site can name a dozen feeds, and a
      // list that runs off the bottom of the screen has no way back.
      // `overscroll-contain` stops a scroll that has reached the end of this dialog carrying
      // on into whatever is behind it. Holding the page still already covers that for a
      // wheel; this is for touch, where `overflow: hidden` on the body is not reliably
      // enough on its own and the gesture becomes a rubber-band or a pull-to-refresh.
      className={`m-auto max-h-[85dvh] overflow-y-auto overscroll-contain rounded-md
        border border-rule p-0 text-ink backdrop:bg-black/50 ${
          onPaper ? "bg-paper" : "bg-paper-raised"
        } ${
          wide
            ? "w-[min(44rem,calc(100vw-2rem))]"
            : "w-[min(28rem,calc(100vw-2rem))]"
        }`}
    >
      {open ? (
        <DialogContext.Provider value={ref}>
          {/*
            The footer is a sibling of the body rather than its last child, so that the two
            can differ — in colour, and in how much room sits above the rule. As a child it
            inherited the body's background and its gap, and `flush` had to be a negative
            margin cancelling that gap from inside it.
          */}
          <div className={`flex flex-col gap-4 px-5 pt-5 ${bodyBottom}`}>
            {/*
              Sized and weighted like a heading, because it is one. At twenty pixels regular
              it was lighter than anything bold underneath it, which in a dialog listing
              articles put the feed's own name below its own samples in the hierarchy. Weight
              alone did not settle it — twenty semibold against nineteen bold is two things
              the same size arguing. The step up in size is what makes it read as a title.
            */}
            <h2 className="font-serif text-2xl font-semibold text-ink">
              {title}
            </h2>
            {children}
          </div>
          {footer ? (
            // Always raised, whatever the body is set on. On an ordinary dialog that is the
            // same colour and changes nothing; under a body on paper it is what makes the
            // buttons read as a bar rather than as the end of the reading.
            <div className="flex flex-wrap items-center justify-end gap-2 border-t border-rule bg-paper-raised px-5 pt-4 pb-5">
              {footer}
            </div>
          ) : null}
        </DialogContext.Provider>
      ) : null}
    </dialog>
  );
}
