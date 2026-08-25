import { useState, type FormEvent } from "react";

import { postLogin } from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";

/**
 * Signing in without leaving the page.
 *
 * The login island sends somebody to a document of its own and then to wherever they were
 * going, which is right when signing in *is* the errand. Here it is not: they are reading
 * somebody's page, and the sign-in is so that they can mark something on it. Being navigated
 * away and brought back would lose their place in a page that may be a hundred articles long,
 * and for a gesture they made in passing.
 *
 * So the same two fields, in a dialog, and nothing navigates. What changes afterwards is the
 * page itself — the masthead becomes the application's, and every card grows a way to mark it
 * read — which is the whole of what signing in was for.
 */
export function SignInDialog({
  open,
  onClose,
}: {
  open: boolean;
  /** Called with true when somebody actually signed in. */
  onClose: (signedIn?: boolean) => void;
}) {
  const callApi = useApiCall();
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
      </form>
    </Modal>
  );
}
