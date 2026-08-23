import { useState } from "react";

import type { Account } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { useBeginRecovery, useConfirmRecovery } from "@app/queries/hooks";

/** How many characters a code is. Mirrors `store.codeLen`. */
const CODE_LENGTH = 8;

/**
 * Proving an address.
 *
 * Two steps, and only the second changes anything: a code goes to the address and has to
 * come back. Until it does, the account has no recovery address at all — not a provisional
 * one — so a flow abandoned anywhere leaves exactly what was there before.
 *
 * That is the whole reason this is not a field with a Save beside it. An address nobody has
 * proved they can read is worse than none: a typo sends recovery to a stranger's inbox, and
 * the owner finds out at the one moment they cannot afford to.
 */
export function RecoveryDialog({
  account,
  onClose,
}: {
  account: Account;
  onClose: () => void;
}) {
  const begin = useBeginRecovery();
  const confirm = useConfirmRecovery();

  // Resumed rather than restarted: an attempt already in flight names the address it is
  // waiting on, and starting somebody over on a code they are holding is a code wasted.
  const [email, setEmail] = useState(
    account.recovery_pending || account.recovery_email,
  );
  const [code, setCode] = useState("");
  // Set once a code is away. The address locks then — changing it would leave the code
  // pointing at the old one, which reads as the flow being further along than it is.
  const [awaiting, setAwaiting] = useState(account.recovery_pending !== "");

  const address = awaiting ? account.recovery_pending || email : email.trim();

  return (
    <Modal
      open
      onClose={onClose}
      title={
        account.recovery_email
          ? "Change your recovery address"
          : "Add a recovery address"
      }
      footer={
        awaiting ? (
          <>
            <Button
              className="mr-auto"
              disabled={begin.isPending}
              onClick={() => begin.mutate(address)}
            >
              {begin.isPending ? "Sending…" : "Send it again"}
            </Button>
            <Button onClick={onClose}>Cancel</Button>
            <Button
              variant="primary"
              disabled={code.trim().length < CODE_LENGTH || confirm.isPending}
              onClick={() =>
                confirm.mutate(code.trim(), { onSuccess: onClose })
              }
            >
              {confirm.isPending ? "Confirming…" : "Confirm"}
            </Button>
          </>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button
              variant="primary"
              disabled={email.trim() === "" || begin.isPending}
              onClick={() =>
                begin.mutate(email.trim(), {
                  onSuccess: () => setAwaiting(true),
                })
              }
            >
              {begin.isPending ? "Sending…" : "Send a code"}
            </Button>
          </>
        )
      }
    >
      <p className="text-sm text-ink-muted">
        A code goes to the address and has to come back, so only an address you
        can actually read counts. Nothing changes until it does.
      </p>

      <Field
        label="Address"
        type="email"
        placeholder="you@example.com"
        autoFocus={!awaiting}
        disabled={awaiting}
        hint={
          awaiting
            ? "Locked while a code is outstanding. Cancel and start again to use a different one."
            : undefined
        }
        value={address}
        onChange={(event) => setEmail(event.target.value)}
      />

      {begin.error ? <Alert>{begin.error.message}</Alert> : null}

      {awaiting ? (
        <>
          <Alert tone="note">
            A code was sent to {address}. It is good for fifteen minutes.
          </Alert>
          <Field
            label="Code"
            hint="Eight characters. Case does not matter."
            className="font-mono"
            maxLength={CODE_LENGTH}
            autoFocus
            placeholder="K7QM2XPT"
            value={code}
            onChange={(event) => setCode(event.target.value)}
          />
          {confirm.error ? <Alert>{confirm.error.message}</Alert> : null}
        </>
      ) : null}
    </Modal>
  );
}
