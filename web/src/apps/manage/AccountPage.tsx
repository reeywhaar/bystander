import { useState } from "react";

import { postLogout } from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button, buttonClasses } from "@app/components/ui/Button";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute } from "@app/lib/time";
import {
  useAccount,
  useForgetRecovery,
  useSetPublicName,
} from "@app/queries/hooks";

import { PasswordDialog } from "@app/apps/manage/PasswordDialog";
import { PublicNameDialog } from "@app/apps/manage/PublicNameDialog";
import { DeleteAccountDialog } from "@app/apps/manage/DeleteAccountDialog";
import { RecoveryDialog } from "@app/apps/manage/RecoveryDialog";
import { SessionsDialog } from "@app/apps/manage/SessionsDialog";

/**
 * The account itself: who you are, how you get back in, and the way out.
 *
 * Signing out lives here rather than in the masthead. It was one slip away from being
 * pressed by somebody aiming at the link beside it, and in exchange for that risk it told
 * nobody anything — a page you go to is the right home for a thing you do once.
 */
export function AccountPage() {
  const account = useAccount();
  const forget = useForgetRecovery();
  const name = useSetPublicName();
  // Whether the naming dialog is up. The same dialog publishing will open when somebody
  // has no name yet, so the question is asked the same way in both places.
  const [naming, setNaming] = useState(false);
  const callApi = useApiCall();

  const [signingOut, setSigningOut] = useState(false);
  // Whether the password dialog is up, and whether it changed anything last time. The second
  // outlives the dialog on purpose: the confirmation belongs on the page somebody is left
  // looking at, not in a box that closes on the same press.
  const [changing, setChanging] = useState(false);
  const [changed, setChanged] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const [proving, setProving] = useState(false);

  if (account.isPending) return <Spinner />;
  if (account.error) throw account.error;
  const me = account.data;

  async function signOut() {
    setSigningOut(true);
    try {
      await callApi(postLogout());
    } finally {
      // Whatever the server said, the cookie is gone or was never valid. Sending them
      // somewhere either way beats leaving them on a page that cannot load.
      //
      // To "/" rather than to "/login", because "/" is now two pages and the one a person
      // without a session gets is the landing page — which is a better thing to hand somebody
      // who has just left than a form asking them to come back. A whole-document navigation,
      // so the server decides which shell that is with the cookie already cleared.
      window.location.href = "/";
    }
  }

  return (
    <div className="flex flex-col gap-10">
      <section className="flex flex-col gap-2">
        <h2 className="font-serif text-xl text-ink">{me.username}</h2>
        <p className="text-sm text-ink-muted">
          {me.role === "admin" ? "An administrator" : "An ordinary account"},
          here since {absolute(me.created_at)}.
        </p>
      </section>

      {/* A deletion is called off by signing in rather than by pressing anything, which
          means it is called off silently. Somebody who asked, forgot, and signed in a
          fortnight later is owed the news — and somebody who did not ask in the first place
          is owed it more. The server only reports this while it is recent, so it goes away
          on its own rather than becoming a permanent notice about a thing that did not
          happen. */}
      {me.deletion_cancelled_at ? (
        <Alert tone="note">
          You asked for this account to be deleted, and signing in on{" "}
          {absolute(me.deletion_cancelled_at)} cancelled it. Nothing was erased.
          If that was not you, change your password.
        </Alert>
      ) : null}

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Password</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Changing it signs out every other device and leaves this one signed
          in. Your current password is required — being signed in here is not
          the same as knowing it.
        </p>

        {/* Said on the page rather than in the dialog, which has closed by now. A
            confirmation that goes with the box that produced it is a confirmation nobody
            reads. */}
        {changed ? (
          <Alert tone="note">
            Changed. Anywhere else you were signed in has been signed out.
          </Alert>
        ) : null}

        <div>
          <Button onClick={() => setChanging(true)}>Change password</Button>
        </div>
      </section>

      {/* Hidden, not disabled. An instance that publishes nothing has no use for a public
          name, and offering one there would be offering a thing that does not exist — the
          administrator's answer is the whole answer, and this is not a control that argues
          with it. */}
      {me.public_pages ? (
        <section className="flex flex-col gap-3 border-t border-rule pt-8">
          <h2 className="font-serif text-xl text-ink">Public name</h2>
          <p className="max-w-prose text-sm text-ink-muted">
            The name any page you publish lives under. Not the name you sign in
            with — that one is a password's other half, and publishing a page is
            no reason to hand it out. Nothing is public until you publish
            something.
          </p>

          <p className="text-sm text-ink">
            {me.public_name === "" ? (
              <span className="text-ink-muted">
                None yet. You will be asked for one the first time you publish a
                page.
              </span>
            ) : (
              <span className="font-mono text-xs text-ink-muted">
                /p/<span className="text-ink">{me.public_name}</span>/…
              </span>
            )}
          </p>

          {name.error ? <Alert>{name.error.message}</Alert> : null}

          <div className="flex flex-wrap gap-2">
            {/* "Change name" rather than "Change it": the recovery address below has a
              "Change it" of its own, and two buttons with one accessible name on a page is a
              list a screen reader cannot tell apart. */}
            <Button onClick={() => setNaming(true)}>
              {me.public_name === "" ? "Choose a name" : "Change name"}
            </Button>
            {me.public_name !== "" ? (
              <Button
                variant="danger"
                disabled={name.isPending}
                onClick={() => name.mutate("")}
              >
                {name.isPending ? "Giving it up…" : "Give it up"}
              </Button>
            ) : null}
          </div>
        </section>
      ) : null}

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
        <h2 className="font-serif text-xl text-ink">Your data</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          A zip holding one JSON file: this account, your tags, every feed you
          follow and what you filed it under, your front pages and what each
          draws from, everything you have read, and everything still waiting.
          Enough to rebuild this somewhere else, or simply to keep.
        </p>
        <p className="max-w-prose text-sm text-ink-muted">
          What you have read reaches back as far as you have followed the feed.
          What is still waiting reaches back only as far as the articles
          themselves are kept, which is thirty days — this is a front page, not
          an archive of everything ever published.
        </p>
        <div>
          {/* A link rather than a button. The archive is written straight to the socket as
              it is read, so letting the browser take it to disk means neither side ever
              holds the whole thing — and the download survives leaving this page. */}
          <a className={buttonClasses()} href="/api/account/export" download>
            Download your data
          </a>
        </div>
      </section>

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Signed in devices</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Every browser holding a session for this account, where it was last
          used from and when. Anything you do not recognise can be signed out
          from here — though a password you did not choose to change is worth
          changing too, since ending a session does not stop somebody who knows
          it from starting another.
        </p>
        <div>
          <Button onClick={() => setReviewing(true)}>
            Review signed in devices
          </Button>
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

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Delete your account</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Nothing goes today. Your account is marked and erased a week later —
          every feed you follow, your tags, your front pages and the record of
          what you have read. Signing in during that week cancels it, which is
          also what makes a deletion you did not ask for something you can undo
          by yourself.
        </p>

        {/* Said plainly rather than left to be discovered. An instance with no administrator
            has no way back that does not involve a shell on the host, so this is the one
            account that cannot go — and finding that out after typing your password into a
            danger button is a worse way to learn it. */}
        {me.last_admin ? (
          <Alert tone="note">
            You are the only administrator here, and an instance with none has
            no way back. Invite somebody else and make them an administrator
            first.
          </Alert>
        ) : null}

        <div>
          <Button
            variant="danger"
            disabled={me.last_admin}
            onClick={() => setLeaving(true)}
          >
            Delete your account…
          </Button>
        </div>
      </section>

      {/* Mounted only while open, so it starts from what is on record every time rather
          than from whatever was typed and abandoned last time. */}
      {proving ? (
        <RecoveryDialog account={me} onClose={() => setProving(false)} />
      ) : null}

      <PasswordDialog
        open={changing}
        onClose={(saved) => {
          setChanging(false);
          if (saved) setChanged(true);
        }}
      />

      <SessionsDialog open={reviewing} onClose={() => setReviewing(false)} />

      {leaving ? (
        <DeleteAccountDialog open onClose={() => setLeaving(false)} />
      ) : null}

      <PublicNameDialog
        account={me}
        open={naming}
        onClose={() => setNaming(false)}
      />
    </div>
  );
}
