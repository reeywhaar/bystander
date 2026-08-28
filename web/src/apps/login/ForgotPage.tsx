import { useState, type FormEvent } from "react";

import { postRecoveries } from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";

import { Frame } from "@app/apps/login/Frame";

/**
 * Asking for a way back into an account.
 *
 * The address, not the name. What this can reach is an inbox somebody has proved they can
 * read, and the account is whichever one is attached to it — so asking for a username would
 * be asking for something this cannot act on, and would quietly become a way to find out
 * which names exist.
 *
 * The confirmation is the same whatever happened: address on file, address unknown, account
 * disabled, relay refused. Anything else turns this form into a way to ask the instance who
 * has an account here. The cost is that a mistyped address is not corrected, and the mail
 * carries that correction instead — it names the account, so a link arriving for a name you
 * do not recognise is its own answer.
 */
export function ForgotPage() {
  const callApi = useApiCall();
  const [email, setEmail] = useState("");
  const [asked, setAsked] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await callApi(postRecoveries(email));
      setAsked(email);
    } catch (failure) {
      // Only the refusals that are about the request itself reach here — a malformed address,
      // or too many tries. Everything about whether an account exists is a success.
      setError(
        failure instanceof Error ? failure.message : "that did not work",
      );
    } finally {
      setBusy(false);
    }
  }

  if (asked !== null) {
    return (
      <Frame title="Look in your inbox">
        <p className="text-sm text-ink-muted">
          If <span className="text-ink">{asked}</span> is the recovery address
          for an account here, a link to set a new password is on its way to it.
          It names the account and works once, and it lapses after a day.
        </p>
        <p className="text-sm text-ink-muted">
          Nothing has changed yet. Your old password still works until you use
          the link.
        </p>
        <p className="text-sm text-ink-muted">
          <a className="text-accent underline" href="/login">
            Back to signing in
          </a>
        </p>
      </Frame>
    );
  }

  return (
    <Frame
      title="Forgotten your password"
      intro="Give the address your account can be recovered through, and a link to set a new password goes there."
    >
      <form
        onSubmit={(event) => void submit(event)}
        className="flex flex-col gap-4"
      >
        {error ? <Alert>{error}</Alert> : null}

        <Field
          label="Address"
          type="email"
          value={email}
          autoComplete="email"
          autoFocus
          required
          placeholder="you@example.com"
          onChange={(event) => setEmail(event.target.value)}
        />
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "Sending…" : "Send me a link"}
        </Button>
      </form>

      {/* The other way in, and on an instance with no relay the only one. Said here rather
          than left to be discovered, because somebody who never set a recovery address has
          no way to find out from this form that it was never going to work. */}
      <p className="text-sm text-ink-muted">
        No address on your account, or nothing arrives? Whoever runs this
        instance can hand you a link directly.
      </p>
    </Frame>
  );
}
