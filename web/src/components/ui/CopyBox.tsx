import { useState } from "react";

import { Modal } from "@app/components/ui/Modal";
import { QRCode } from "@app/components/ui/QRCode";

/**
 * A link, and the three ways it leaves this machine.
 *
 * Copy is the one that works everywhere and reaches nothing but this device. Send hands it
 * to the system, which is how a link actually gets to a phone — AirDrop, a message, whatever
 * that person uses. QR is for when the other device is in the room and neither of the first
 * two will do: two accounts on two phones with no shared anything.
 *
 * The value stays selectable text through all of it. Every one of these can fail — an
 * insecure origin, a browser that refuses without a gesture it recognises — and a button
 * that silently does nothing is worse than no button.
 */
export function CopyBox({
  value,
  /** What the system share sheet calls this, when there is one. */
  shareTitle,
}: {
  value: string;
  shareTitle?: string;
}) {
  const [copied, setCopied] = useState(false);
  const [showing, setShowing] = useState(false);

  // Feature-detected rather than assumed. `navigator.share` needs a secure context, so it is
  // simply absent when an instance is reached over plain http on a local address — which is
  // exactly the setup somebody self-hosting is most likely to have. Offering a button that
  // cannot work there is worse than not offering one.
  const canShare = typeof navigator !== "undefined" && "share" in navigator;

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  }

  async function send() {
    try {
      await navigator.share({ title: shareTitle, url: value });
    } catch {
      // Includes the ordinary case: the sheet opened and was closed again. There is nothing
      // to report about a share that was not made.
    }
  }

  const button =
    "shrink-0 rounded border border-rule bg-paper-raised px-2 py-1 text-xs text-ink-muted hover:text-ink";

  return (
    <>
      <div className="flex items-center gap-2 rounded-md border border-rule bg-paper-sunken p-2">
        <code className="flex-1 overflow-x-auto text-xs break-all text-ink select-all">
          {value}
        </code>
        <button
          type="button"
          onClick={() => setShowing(true)}
          className={button}
        >
          QR
        </button>
        {canShare ? (
          <button type="button" onClick={() => void send()} className={button}>
            Send
          </button>
        ) : null}
        <button type="button" onClick={() => void copy()} className={button}>
          {copied ? "Copied" : "Copy"}
        </button>
      </div>

      {showing ? (
        <Modal
          open
          onClose={() => setShowing(false)}
          title={shareTitle ?? "This link"}
          footer={
            <button
              type="button"
              onClick={() => setShowing(false)}
              className={button}
            >
              Done
            </button>
          }
        >
          <p className="text-sm text-ink-muted">
            Point the other device's camera at this.
          </p>
          {/* Wide as the dialog allows: a code photographed off a screen wants every module
              it can get, and this is the only thing on screen while it is up. */}
          <QRCode
            value={value}
            className="mx-auto w-full max-w-72 rounded bg-white p-3"
          />
          <code className="text-xs break-all text-ink-faint select-all">
            {value}
          </code>
        </Modal>
      ) : null}
    </>
  );
}
