import { useState, type FormEvent } from "react";

import type { Role } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { CopyBox } from "@app/components/ui/CopyBox";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { Segmented } from "@app/components/ui/Segmented";
import { useCreateInvite, useSmtp } from "@app/queries/hooks";

/**
 * Making an invitation: what kind of account, and how it reaches them.
 *
 * A dialog rather than a strip of controls above the list, because the second question changes
 * what the first form even is — an address field appears, the button changes what it does, and
 * afterwards there is either a link to copy or a message to read. Growing and shrinking a
 * toolbar under a table moves the table, and a table that moves while somebody is reading it
 * is the thing this is here to avoid.
 *
 * Three states, one after another: the form, then either the link or the confirmation. Never
 * both — see the comment on the two deliveries below.
 */
export function InviteDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const create = useCreateInvite();
  // Only for whether a relay exists. The admin island reads this for the Mail tab, so it is
  // usually already cached by the time anybody opens this.
  const smtp = useSmtp();

  // The state is the answer, not the position of the answer. Segmented speaks in indexes
  // because that is all a row of labels can mean on its own; a form holding an index would be
  // a form that has to be read alongside the array to know what it says.
  const [role, setRole] = useState<Role>("user");
  const [byEmail, setByEmail] = useState(false);
  const [email, setEmail] = useState("");
  // What came back. A link to copy, or the address it went to — never both, which is the whole
  // of what makes an emailed invitation proof of that address.
  const [minted, setMinted] = useState<string | null>(null);
  const [sent, setSent] = useState<string | null>(null);

  const canSend = smtp.data?.configured ?? false;
  const done = minted !== null || sent !== null;

  function reset() {
    setRole("user");
    setByEmail(false);
    setEmail("");
    setMinted(null);
    setSent(null);
    create.reset();
  }

  function close() {
    reset();
    onClose();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { role, email: byEmail ? email : "" },
      {
        onSuccess: (invite) => {
          if (invite.email) setSent(invite.email);
          else setMinted(invite.url ?? null);
        },
      },
    );
  }

  return (
    <Modal
      open={open}
      onClose={close}
      title={done ? "Invitation made" : "Create invitation"}
      footer={
        <div className="flex flex-wrap items-center gap-3">
          {done ? (
            <>
              <Button variant="primary" onClick={close}>
                Done
              </Button>
              <Button onClick={reset}>Make another</Button>
            </>
          ) : (
            <>
              <Button
                variant="primary"
                type="submit"
                form="invite-form"
                disabled={create.isPending || (byEmail && !canSend)}
              >
                {create.isPending
                  ? byEmail
                    ? "Sending…"
                    : "Minting…"
                  : "Create"}
              </Button>
              <Button onClick={close}>Cancel</Button>
            </>
          )}
        </div>
      }
    >
      {done ? (
        <div className="flex flex-col gap-3">
          {minted ? (
            <>
              <CopyBox value={minted} shareTitle="An invitation to bystander" />
              <p className="text-xs text-ink-muted">
                Take this now — it is the only time it can be read. What is
                stored is a hash, so a lost link is replaced rather than
                recovered.
              </p>
            </>
          ) : (
            <p className="text-sm text-ink-muted">
              Sent to <span className="text-ink">{sent}</span>. It works once
              and lapses in a week, and becomes that account&rsquo;s recovery
              address when they accept.
            </p>
          )}
        </div>
      ) : (
        <form
          id="invite-form"
          onSubmit={submit}
          className="flex flex-col gap-5"
        >
          <Segmented
            label="What kind of account"
            options={["Ordinary", "Administrator"]}
            value={role === "admin" ? 1 : 0}
            onChange={(index) => setRole(index === 1 ? "admin" : "user")}
          />

          <Segmented
            label="How it reaches them"
            options={["Link", "Email"]}
            value={byEmail ? 1 : 0}
            onChange={(index) => setByEmail(index === 1)}
          />

          {/* The gate, reached by choosing to send rather than by finding the control dead.
              "Send it to an address" is a reasonable thing to want, and a greyed button that
              says nothing leaves somebody looking for the reason in the wrong place. */}
          {byEmail && smtp.data && !canSend ? (
            <Alert tone="note">
              No mail relay is configured, so this instance cannot send anything
              yet. Set one up under{" "}
              <a href="/admin/mail" className="underline underline-offset-2">
                Mail
              </a>
              , or choose Link and pass it on yourself.
            </Alert>
          ) : null}

          {byEmail ? (
            <Field
              label="Send it to"
              type="email"
              required
              autoFocus
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              placeholder="them@example.com"
              disabled={!canSend}
              hint="This becomes the account's recovery address when they accept, because the invitation reached them at it. The link is not shown here — that is what makes it proof."
            />
          ) : (
            <p className="text-sm text-ink-muted">
              You get a link to pass on however you like. It works once, and
              lapses in a week.
            </p>
          )}

          {create.error ? <Alert>{create.error.message}</Alert> : null}
        </form>
      )}
    </Modal>
  );
}
