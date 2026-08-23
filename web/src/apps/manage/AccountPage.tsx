import { useState } from "react";

import { postLogout } from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute } from "@app/lib/time";
import {
  useAccount,
  useChangePassword,
  useForgetRecovery,
} from "@app/queries/hooks";

import { RecoveryDialog } from "@app/apps/manage/RecoveryDialog";

/** What the server will refuse anything shorter than. Mirrors `store.MinPasswordLen`. */
const MIN_PASSWORD = 8;

/**
 * The account itself: who you are, how you get back in, and the way out.
 *
 * Signing out lives here rather than in the masthead. It was one slip away from being
 * pressed by somebody aiming at the link beside it, and in exchange for that risk it told
 * nobody anything — a page you go to is the right home for a thing you do once.
 */
export function AccountPage() {
  const account = useAccount();
  const change = useChangePassword();
  const forget = useForgetRecovery();
  const callApi = useApiCall();

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [signingOut, setSigningOut] = useState(false);
  const [proving, setProving] = useState(false);

  if (account.isPending) return <Spinner />;
  if (account.error) throw account.error;
  const me = account.data;

  async function signOut() {
    setSigningOut(true);
    try {
      await callApi(postLogout());
    } finally {
      // Whatever the server said, the cookie is gone or was never valid. Sending them to
      // the login island either way beats leaving them on a page that cannot load.
      window.location.href = "/login";
    }
  }

  // Caught here rather than by the server, because the server cannot: it receives one new
  // password and has no way to know it was typed twice.
  const mismatch = again !== "" && next !== again;
  const canChange =
    current !== "" &&
    next.length >= MIN_PASSWORD &&
    next === again &&
    !change.isPending;

  return (
    <div className="flex flex-col gap-10">
      <section className="flex flex-col gap-2">
        <h2 className="font-serif text-xl text-ink">{me.username}</h2>
        <p className="text-sm text-ink-muted">
          {me.role === "admin" ? "An administrator" : "An ordinary account"},
          here since {absolute(me.created_at)}.
        </p>
      </section>

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Change your password</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Your current one is required — being signed in here is not the same as
          knowing it, and the difference is what stops a borrowed session
          becoming a taken account. Your other devices are signed out; this one
          stays.
        </p>

        <div className="grid max-w-sm gap-4">
          <Field
            label="Current password"
            type="password"
            autoComplete="current-password"
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
        </div>

        {change.error ? <Alert>{change.error.message}</Alert> : null}
        {change.isSuccess ? (
          <Alert tone="note">
            Changed. Anywhere else you were signed in has been signed out.
          </Alert>
        ) : null}

        <div>
          <Button
            variant="primary"
            disabled={!canChange}
            onClick={() =>
              change.mutate(
                { current_password: current, new_password: next },
                {
                  // Nothing is kept afterwards. These are three password fields, and
                  // leaving them filled leaves them to be read over a shoulder.
                  onSuccess: () => {
                    setCurrent("");
                    setNext("");
                    setAgain("");
                  },
                },
              )
            }
          >
            {change.isPending ? "Changing…" : "Change it"}
          </Button>
        </div>
      </section>

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Recovery address</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Somewhere to reach you if you forget your password. Nothing else is
          ever sent here, and it is not a way to sign in — only a way back in.
        </p>

        {/* Said plainly rather than left to be discovered. Adding an address on an instance
            that cannot send would be a promise nobody can keep, and the moment somebody
            finds that out is the moment they are already locked out. */}
        {!me.mail_configured ? (
          <Alert tone="note">
            No mail relay is configured on this instance yet, so an address
            cannot be confirmed. An administrator sets one up under Admin →
            Mail.
          </Alert>
        ) : null}

        <p className="text-sm text-ink">
          {me.recovery_email === "" ? (
            <span className="text-ink-muted">
              None. Without one, a forgotten password needs an administrator.
            </span>
          ) : (
            me.recovery_email
          )}
        </p>

        {/* Named, so reopening resumes rather than starting somebody over on a code they
            are already holding. */}
        {me.recovery_pending ? (
          <p className="text-xs text-ink-faint">
            Waiting on a code sent to {me.recovery_pending}.
          </p>
        ) : null}

        {forget.error ? <Alert>{forget.error.message}</Alert> : null}

        <div className="flex flex-wrap gap-2">
          <Button
            disabled={!me.mail_configured}
            onClick={() => setProving(true)}
          >
            {me.recovery_pending
              ? "Finish confirming"
              : me.recovery_email === ""
                ? "Add an address"
                : "Change it"}
          </Button>
          {me.recovery_email || me.recovery_pending ? (
            <Button
              variant="danger"
              disabled={forget.isPending}
              onClick={() => forget.mutate()}
            >
              {forget.isPending ? "Removing…" : "Remove it"}
            </Button>
          ) : null}
        </div>
      </section>

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Sign out</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Ends this session only. Anywhere else you are signed in stays that
          way.
        </p>
        <div>
          <Button
            variant="danger"
            disabled={signingOut}
            onClick={() => void signOut()}
          >
            {signingOut ? "Signing out…" : "Sign out"}
          </Button>
        </div>
      </section>

      {/* Mounted only while open, so it starts from what is on record every time rather
          than from whatever was typed and abandoned last time. */}
      {proving ? (
        <RecoveryDialog account={me} onClose={() => setProving(false)} />
      ) : null}
    </div>
  );
}
