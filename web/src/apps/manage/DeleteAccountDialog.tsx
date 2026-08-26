import { useState } from "react";

import { Alert } from "@app/components/ui/Alert";
import { Button, buttonClasses } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { absolute } from "@app/lib/time";
import { useDeleteAccount } from "@app/queries/hooks";

/**
 * How long the confirmation stays up before the browser is sent to "/".
 *
 * Long enough to read a date and short enough that nobody is left sitting on a page whose
 * every control now answers 401.
 */
const LEAVE_AFTER = 6000;

/**
 * Leaving.
 *
 * Nothing is erased when this is pressed. The account is marked, a week passes, and only
 * then does it go — and signing in at any point during that week calls the whole thing off
 * by itself. That delay is not politeness. "Delete my account" pressed by mistake, or
 * pressed by somebody who should not have the session, has to be recoverable, and the only
 * recovery that works for a person who has lost both their password and their account is one
 * they can perform without asking anybody.
 *
 * The password is asked for because being signed in is not the same as knowing it — the same
 * reason changing one asks. The export is offered here rather than left to be remembered,
 * because the moment somebody decides to leave is the last moment the offer is any use.
 */
export function DeleteAccountDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const leave = useDeleteAccount();
  const [password, setPassword] = useState("");

  const scheduled = leave.data;

  function confirm() {
    // `mutate` with a callback rather than awaiting `mutateAsync`: a refused password
    // rejects, the rejection is already being rendered from `leave.error`, and awaiting it
    // without a catch is an unhandled rejection in every browser that runs this.
    leave.mutate(password, {
      onSuccess: () => {
        // Every session ended with the request, including this one. A whole-document
        // navigation rather than a route change, because the next page is served to
        // somebody without a cookie and only the server decides what that is.
        //
        // Left on screen for a moment first: the date is the one thing worth reading, and
        // the page underneath is already signed out.
        window.setTimeout(() => window.location.assign("/"), LEAVE_AFTER);
      },
    });
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Delete your account"
      footer={
        scheduled ? (
          <Button onClick={() => window.location.assign("/")}>Close</Button>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={leave.isPending || password === ""}
              onClick={confirm}
            >
              {leave.isPending ? "Asking…" : "Delete my account"}
            </Button>
          </div>
        )
      }
    >
      {scheduled ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-ink">
            Your account will be erased on {absolute(scheduled.purge_at)}. You
            have been signed out everywhere.
          </p>
          <p className="text-sm text-ink-muted">
            Signing in again before then cancels it, and there is no button to
            find — signing in is all it takes.
          </p>
          {/* Said rather than implied. The message is the safety net for the case this most
              needs one, and an account with no address on file has no safety net. */}
          <p className="text-sm text-ink-muted">
            {scheduled.notified
              ? "A note saying the same thing has gone to your recovery address."
              : "There is no recovery address on this account, so nothing was sent. This dialog is the only notice you will get."}
          </p>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <p className="text-sm text-ink">
            Nothing is erased today. Your account is marked, and a week from now
            it and everything in it — every feed you follow, your tags, your
            front pages and the record of what you have read — is removed for
            good.
          </p>
          <p className="text-sm text-ink-muted">
            You are signed out everywhere as soon as you ask. Signing in again
            at any point during that week cancels it: there is no button to
            find, and no one to ask. If you do nothing, the account goes.
          </p>
          {/* Offered here rather than left to be remembered. This is the last moment the
              offer is any use to anybody. */}
          <Alert tone="note">
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
              Take a copy first — you cannot come back for it.
              <a
                className={buttonClasses()}
                href="/api/account/export"
                download
              >
                Download your data
              </a>
            </span>
          </Alert>

          {leave.error ? <Alert>{leave.error.message}</Alert> : null}

          <Field
            label="Your password"
            type="password"
            autoComplete="current-password"
            hint="Being signed in is not the same as knowing it."
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>
      )}
    </Modal>
  );
}
