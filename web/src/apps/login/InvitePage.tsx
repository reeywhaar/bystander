import { useQuery } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { useParams } from "react-router";

import {
  getInvitesByToken,
  postInvitesByTokenAccept,
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
 * Accepting an invitation.
 *
 * The link's state is read before anybody types a password into it, because valid, expired
 * and already-accepted are three situations a person acts on differently — wait, ask for a
 * new link, or just sign in — and collapsing them into one refusal leaves all three doing
 * the same useless thing.
 */
export function InvitePage() {
  const { token } = useParams<{ token: string }>();
  const callApi = useApiCall();

  const invite = useQuery({
    queryKey: qk.invite(token ?? ""),
    queryFn: ({ signal }) => callApi(getInvitesByToken(token ?? ""), signal),
    enabled: Boolean(token),
  });

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // A truncated link — messaging apps cut long URLs — lands here with no token at all.
  if (!token) {
    return (
      <Frame title="That link looks incomplete">
        <p className="text-sm text-ink-muted">
          An invitation link ends in a long code. Yours appears to have been cut
          short, which chat apps do to long links. Ask whoever sent it to paste
          it again.
        </p>
      </Frame>
    );
  }

  if (invite.isPending) return <Spinner label="Checking your invitation" />;

  if (invite.error) {
    return (
      <Frame title="That invitation is not one of ours">
        <Alert>{invite.error.message}</Alert>
        <p className="text-sm text-ink-muted">
          If you already have an account,{" "}
          <a className="text-accent underline" href="/login">
            sign in
          </a>
          .
        </p>
      </Frame>
    );
  }

  if (invite.data.accepted) {
    return (
      <Frame title="That invitation has already been used">
        <p className="text-sm text-ink-muted">
          An invitation creates one account and then it is spent. If it was you
          who used it,{" "}
          <a className="text-accent underline" href="/login">
            sign in
          </a>
          .
        </p>
      </Frame>
    );
  }

  if (invite.data.expired) {
    return (
      <Frame title="That invitation has expired">
        <p className="text-sm text-ink-muted">
          It lapsed on {absolute(invite.data.expires_at)}. Ask whoever sent it
          for a new link — they are quick to make.
        </p>
      </Frame>
    );
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await callApi(postInvitesByTokenAccept(token!, username, password));
      // Accepting signs them in: they have just chosen a password, so asking for it again
      // proves nothing and is one more place to lose somebody.
      window.location.href = "/";
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "that did not work",
      );
      setBusy(false);
    }
  }

  const sentTo = invite.data.email;

  return (
    <Frame
      title="Choose a name and a password"
      intro={
        invite.data.role === "admin"
          ? "This invitation makes you an administrator of this instance."
          : "You have been invited to read here."
      }
    >
      <form
        onSubmit={(event) => void submit(event)}
        className="flex flex-col gap-4"
      >
        {error ? <Alert>{error}</Alert> : null}

        {/* Said before the account exists, not after. This link reached that inbox, which is
            the whole proof, so accepting attaches the address to the account without asking
            for it again — and somebody should be told what their account is being attached to
            while they can still close the tab. */}
        {sentTo ? (
          <p className="text-sm text-ink-muted">
            This invitation was sent to{" "}
            <span className="text-ink">{sentTo}</span>, so it becomes the
            recovery address for your account. You can change it later under
            Settings.
          </p>
        ) : null}

        <Field
          label="Name"
          value={username}
          autoComplete="username"
          autoFocus
          required
          hint="Letters, digits, and - _ . — between 2 and 32 characters."
          onChange={(event) => setUsername(event.target.value)}
        />
        <Field
          label="Password"
          type="password"
          value={password}
          autoComplete="new-password"
          required
          minLength={8}
          hint="At least 8 characters."
          onChange={(event) => setPassword(event.target.value)}
        />
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "Creating your account…" : "Create my account"}
        </Button>
      </form>
    </Frame>
  );
}
