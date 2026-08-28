import { useQuery } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { useParams } from "react-router";

import {
  getRecoveriesByToken,
  postRecoveriesByTokenAccept,
} from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute } from "@app/lib/time";
import { qk } from "@app/queries/keys";

import { Frame } from "@app/apps/login/Frame";

/**
 * Setting a new password from a recovery link.
 *
 * The link's state is read before anybody types into it, for the reason the invitation page
 * does the same: used, superseded and expired are situations a person acts on differently —
 * sign in with the password you already set, open the newer link, ask for another — and one
 * shared refusal leaves all three doing nothing useful.
 *
 * It ends at the login form rather than signed in, and that is the difference from accepting
 * an invitation. There the account did not exist a moment ago and the person choosing the
 * password is definitionally its owner. Here it existed, the link may have reached the wrong
 * inbox, and typing the new password once is the cheapest confirmation that the right person
 * is holding it.
 */
export function RecoverPage() {
  const { token } = useParams<{ token: string }>();
  const callApi = useApiCall();

  const recovery = useQuery({
    queryKey: qk.recovery(token ?? ""),
    queryFn: ({ signal }) => callApi(getRecoveriesByToken(token ?? ""), signal),
    enabled: Boolean(token),
  });

  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  // A truncated link — mail clients wrap long URLs — lands here with no token at all.
  if (!token) {
    return (
      <Frame title="That link looks incomplete">
        <p className="text-sm text-ink-muted">
          A recovery link ends in a long code. Yours appears to have been cut
          short, which mail clients do to long links. Copy it out of the message
          rather than clicking it, or{" "}
          <a className="text-accent underline" href="/forgot">
            ask for another
          </a>
          .
        </p>
      </Frame>
    );
  }

  if (done) {
    return (
      <Frame title="That is your new password">
        <p className="text-sm text-ink-muted">
          Everywhere that was signed in to this account has been signed out, so
          sign in again with the password you just chose.
        </p>
        <p className="text-sm text-ink-muted">
          <a className="text-accent underline" href="/login">
            Sign in
          </a>
        </p>
      </Frame>
    );
  }

  if (recovery.isPending) return <Spinner label="Checking your link" />;

  if (recovery.error) {
    return (
      <Frame title="That recovery link is not one of ours">
        <Alert>{recovery.error.message}</Alert>
        <p className="text-sm text-ink-muted">
          <a className="text-accent underline" href="/forgot">
            Ask for another
          </a>
          , or{" "}
          <a className="text-accent underline" href="/login">
            sign in
          </a>{" "}
          if you have remembered.
        </p>
      </Frame>
    );
  }

  if (recovery.data.used) {
    return (
      <Frame title="That link has already been used">
        <p className="text-sm text-ink-muted">
          A recovery link sets one password and is then spent. If it was you who
          used it,{" "}
          <a className="text-accent underline" href="/login">
            sign in
          </a>{" "}
          with the password you chose.
        </p>
      </Frame>
    );
  }

  // Not the same thing as used, and worth its own sentence: this link was fine and somebody
  // spent a different one, which is either you a moment ago or somebody you should know about.
  if (recovery.data.voided) {
    return (
      <Frame title="That link was replaced">
        <p className="text-sm text-ink-muted">
          A newer link for this account was used, which retires every older one.
          If that was you,{" "}
          <a className="text-accent underline" href="/login">
            sign in
          </a>{" "}
          with the password you chose. If it was not,{" "}
          <a className="text-accent underline" href="/forgot">
            ask for a link of your own
          </a>{" "}
          straight away.
        </p>
      </Frame>
    );
  }

  if (recovery.data.expired) {
    return (
      <Frame title="That link has expired">
        <p className="text-sm text-ink-muted">
          It lapsed on {absolute(recovery.data.expires_at)}. Recovery links are
          good for a day.{" "}
          <a className="text-accent underline" href="/forgot">
            Ask for another
          </a>
          .
        </p>
      </Frame>
    );
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    // Checked here and not on the server, because the server has only one of them. The repeat
    // exists to catch a typo in a password nobody can see, which is a question about this form.
    if (password !== repeat) {
      setError("those two do not match");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await callApi(postRecoveriesByTokenAccept(token!, password));
      setDone(true);
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "that did not work",
      );
      setBusy(false);
    }
  }

  return (
    <Frame
      title="Choose a new password"
      intro={
        <>
          For the account{" "}
          <span className="text-ink">{recovery.data.username}</span>. If that is
          not you, close this and tell whoever sent you the link.
        </>
      }
    >
      <form
        onSubmit={(event) => void submit(event)}
        className="flex flex-col gap-4"
      >
        {error ? <Alert>{error}</Alert> : null}

        <Field
          label="New password"
          type="password"
          value={password}
          autoComplete="new-password"
          autoFocus
          required
          minLength={8}
          hint="At least 8 characters."
          onChange={(event) => setPassword(event.target.value)}
        />
        <Field
          label="Again"
          type="password"
          value={repeat}
          autoComplete="new-password"
          required
          minLength={8}
          onChange={(event) => setRepeat(event.target.value)}
        />
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "Setting it…" : "Set my password"}
        </Button>
      </form>

      <p className="text-xs text-ink-muted">
        This signs out every device that was signed in to this account,
        including whoever you are recovering it from.
      </p>
    </Frame>
  );
}
