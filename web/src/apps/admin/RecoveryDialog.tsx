import { useEffect } from "react";

import type { User } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { CopyBox } from "@app/components/ui/CopyBox";
import { Modal } from "@app/components/ui/Modal";
import { useCreateUserRecovery } from "@app/queries/hooks";

/**
 * Handing somebody a way back into their account.
 *
 * The link is minted when the dialog opens rather than behind a second button. There is only
 * one thing to decide and it was decided by opening this — a form whose only control says
 * "yes, the thing you just asked for" is a step that exists to be clicked through.
 *
 * That is safe because minting one changes nothing: no session ends, no password moves, and
 * the account holder is not told. An administrator can answer "I am locked out" without
 * locking out somebody who turns out to have been fine, and a link nobody uses simply lapses.
 */
export function RecoveryDialog({
  user,
  onClose,
}: {
  /** The account to mint for, or null when the dialog is shut. */
  user: User | null;
  onClose: () => void;
}) {
  const create = useCreateUserRecovery();
  const { mutate, reset } = create;

  useEffect(() => {
    if (!user) return;
    reset();
    mutate(user.id);
  }, [user, mutate, reset]);

  return (
    <Modal
      open={user !== null}
      onClose={onClose}
      title={user ? `A way back in for ${user.username}` : "Recovery link"}
      footer={
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        {create.isPending ? (
          <p className="text-sm text-ink-muted">Minting…</p>
        ) : null}

        {create.error ? <Alert>{create.error.message}</Alert> : null}

        {create.data ? (
          <>
            <CopyBox
              value={create.data.url}
              shareTitle="A way back into your bystander account"
            />
            <p className="text-xs text-ink-muted">
              Take this now — it is the only time it can be read. What is stored
              is a hash, so a lost link is replaced rather than recovered.
            </p>
            <p className="text-xs text-ink-muted">
              It works once, lapses after a day, and sets a new password without
              needing the old one. Until it is used nothing has changed: the
              account&rsquo;s password still works and nobody has been signed
              out. Using it signs out every device that account was signed in
              on.
            </p>
          </>
        ) : null}
      </div>
    </Modal>
  );
}
