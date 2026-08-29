import { useEffect, useState } from "react";

import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { useChangePassword } from "@app/queries/hooks";

/** What the server will refuse anything shorter than. Mirrors `store.MinPasswordLen`. */
const MIN_PASSWORD = 8;

/**
 * Changing your own password.
 *
 * A dialog rather than three fields sitting open on the account page, and the reason is what
 * the fields are. Everything else on that page is something to read — your name, where your
 * pages are published, the address you could be recovered through — and three empty password
 * boxes in the middle of it are the only part that looks like work outstanding. They are also
 * three password boxes on screen for as long as the page is, which is a thing to leave a
 * shared machine showing.
 *
 * The current password is required, and that is the whole point of the form: being signed in
 * is not the same as knowing it, and the difference is what stops a borrowed session becoming
 * a taken account. Asking behind a button does not weaken that — it is the same check, made in
 * a box that is shut most of the time.
 */
export function PasswordDialog({
  open,
  onClose,
}: {
  open: boolean;
  /** Given true when the password actually changed, so the page can say so. */
  onClose: (changed?: boolean) => void;
}) {
  const change = useChangePassword();
  const { reset } = change;

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");

  // Emptied when it opens, not when it closes. These are three password fields, and a dialog
  // cleared on the way out shows what was typed for as long as it takes to close.
  useEffect(() => {
    if (!open) return;
    setCurrent("");
    setNext("");
    setAgain("");
    reset();
  }, [open, reset]);

  // Caught here rather than by the server, because the server cannot: it receives one new
  // password and has no way to know it was typed twice.
  const mismatch = again !== "" && next !== again;
  const usable =
    current !== "" &&
    next.length >= MIN_PASSWORD &&
    next === again &&
    !change.isPending;

  function submit() {
    if (!usable) return;
    change.mutate(
      { current_password: current, new_password: next },
      {
        onSuccess: () => {
          // Nothing is kept afterwards, and the dialog goes with it — leaving three filled
          // password fields on screen is leaving them to be read over a shoulder.
          setCurrent("");
          setNext("");
          setAgain("");
          onClose(true);
        },
      },
    );
  }

  return (
    <Modal
      open={open}
      onClose={() => onClose()}
      title="Change your password"
      footer={
        <>
          <Button onClick={() => onClose()} disabled={change.isPending}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="change-password"
            variant="primary"
            disabled={!usable}
          >
            {change.isPending ? "Changing…" : "Change it"}
          </Button>
        </>
      }
    >
      <form
        id="change-password"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
        className="flex flex-col gap-4"
      >
        <p className="text-sm text-ink-muted">
          Your current one is required — being signed in here is not the same as
          knowing it, and the difference is what stops a borrowed session
          becoming a taken account. Your other devices are signed out; this one
          stays.
        </p>

        <Field
          label="Current password"
          type="password"
          autoComplete="current-password"
          autoFocus
          value={current}
          onChange={(event) => setCurrent(event.target.value)}
        />
        <Field
          label="New password"
          type="password"
          autoComplete="new-password"
          hint={`At least ${MIN_PASSWORD} characters.`}
          value={next}
          onChange={(event) => setNext(event.target.value)}
        />
        <Field
          label="New password again"
          type="password"
          autoComplete="new-password"
          error={mismatch ? "These two do not match." : undefined}
          value={again}
          onChange={(event) => setAgain(event.target.value)}
        />

        {change.error ? <Alert>{change.error.message}</Alert> : null}
      </form>
    </Modal>
  );
}
