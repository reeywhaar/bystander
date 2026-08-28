import { useQuery } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { getPublicInstance, postLogin } from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { qk } from "@app/queries/keys";

/**
 * Signing in without leaving the page.
 *
 * Used from the two documents a person without a session can be looking at: somebody's
 * published page, and the landing page at "/".
 *
 * A dialog rather than a trip to the login island, because on neither of them is signing in
 * *the errand*. On a published page they are reading, and the sign-in is so they can mark
 * something on it; being navigated away and brought back would lose their place in a page a
 * hundred articles long, for a gesture made in passing. On the landing page they are deciding
 * whether they have an account here at all, and sending them to a second document to find out
 * is a worse answer than two fields.
 *
 * What happens *after* differs, and belongs to the caller rather than here — see `onClose`.
 * The published page stays where it is and asks the server again; the landing page navigates
 * to "/", because the document it is in is the one the server hands to a stranger.
 */
export function SignInDialog({
  open,
  onClose,
}: {
  open: boolean;
  /**
   * Called with true when somebody actually signed in, and false when they gave up.
   *
   * What to do with that is the caller's: stay and re-ask, or navigate. Both callers want
   * something different and neither wants the other's.
   */
  onClose: (signedIn?: boolean) => void;
}) {
  const callApi = useApiCall();
  // Whether this instance can mail somebody a way back into their account, asked only once
  // the dialog is open. Both callers mount this permanently and toggle `open`, so without
  // the gate every stranger arriving at a published page would pay for a question nobody
  // has asked yet.
  const instance = useQuery({
    queryKey: qk.publicInstance,
    queryFn: ({ signal }) => callApi(getPublicInstance(), signal),
    enabled: open,
    retry: false,
  });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await callApi(postLogin(username, password));
      setPassword("");
      setBusy(false);
      onClose(true);
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "that did not work",
      );
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={() => onClose()}
      title="Sign in"
      footer={
        <>
          <Button onClick={() => onClose()} disabled={busy}>
            Cancel
          </Button>
          {/* Outside the form, so it is wired to it by name rather than by nesting — a
              dialog's buttons live in its footer and the fields live in its body. */}
          <Button
            type="submit"
            form="sign-in"
            variant="primary"
            disabled={busy}
          >
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </>
      }
    >
      <form
        id="sign-in"
        onSubmit={(event) => void submit(event)}
        className="flex flex-col gap-4"
      >
        <p className="text-sm text-ink-muted">
          You will stay on this page. Signing in is only so that what you read
          here counts as read.
        </p>

        {error ? <Alert>{error}</Alert> : null}

        <Field
          label="Name"
          value={username}
          autoComplete="username"
          autoFocus
          required
          onChange={(event) => setUsername(event.target.value)}
        />
        <Field
          label="Password"
          type="password"
          value={password}
          autoComplete="current-password"
          required
          onChange={(event) => setPassword(event.target.value)}
        />

        {/* An ordinary link out, and it has to be: /forgot lives in the login island, which
            is a different document from either of the two this dialog opens over. Offered
            only where a relay exists — a form that takes an address and says "check your
            inbox" on an instance that cannot send is a promise to somebody already locked
            out. */}
        {instance.data?.recovery ? (
          <p className="text-sm text-ink-muted">
            <a className="text-accent underline" href="/forgot">
              Forgotten your password?
            </a>
          </p>
        ) : null}
      </form>
    </Modal>
  );
}
