import { useEffect, useState } from "react";

import type { Account } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { useSetPublicName } from "@app/queries/hooks";

/** What a public name is allowed to be, matching what the server will accept. */
const SHAPE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/**
 * Proposes a name from what somebody typed, so nobody has to think about URLs.
 *
 * The same shape the page addresses use, and the same reason: this ends up between two slashes
 * and two spellings of one name would be two addresses for one person.
 */
function tidy(typed: string): string {
  return typed
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

/**
 * Chooses the name somebody's published pages live under.
 *
 * A second name, not the username. Two names for two jobs: one to sign in with, one to be known
 * by — and the one to sign in with is a credential half the world reuses, which is not a thing
 * publishing a page should oblige anybody to announce.
 *
 * A dialog rather than a field on the account page, because it is asked for in two places: here,
 * deliberately, and again the first time somebody publishes a page without having one. Asking
 * the same question the same way in both is what stops the second one feeling like an
 * interruption.
 */
export function PublicNameDialog({
  account,
  open,
  onClose,
}: {
  account: Account;
  open: boolean;
  /** Given the name that was saved, when one was. */
  onClose: (saved?: string) => void;
}) {
  const set = useSetPublicName();
  const [name, setName] = useState(account.public_name);

  // Reset whenever it opens, so a name abandoned last time is not sitting in the field.
  useEffect(() => {
    if (open) setName(account.public_name);
  }, [open, account.public_name]);

  const proposed = tidy(name);
  const usable = proposed !== "" && SHAPE.test(proposed);
  const changing =
    account.public_name !== "" && proposed !== account.public_name;

  const save = () =>
    set.mutate(proposed, { onSuccess: () => onClose(proposed) });

  return (
    <Modal
      open={open}
      onClose={() => onClose()}
      title={
        account.public_name === ""
          ? "Choose a public name"
          : "Change your public name"
      }
      footer={
        <>
          <Button onClick={() => onClose()} disabled={set.isPending}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={save}
            disabled={!usable || set.isPending}
          >
            {set.isPending ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-ink-muted">
          The name your published pages live under. Not the name you sign in
          with — that one is a password's other half, and publishing a page is
          no reason to hand it out.
        </p>

        <Field
          label="Public name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="misha"
          maxLength={40}
          autoFocus
          hint={
            proposed === ""
              ? "Lowercase letters, numbers and hyphens."
              : `Your pages will be at /p/${proposed}/…`
          }
        />

        {/* Said before it is pressed rather than discovered afterwards. Nothing stores the
            address — it is built from this name every time — so changing it moves every
            published page at once, and every link anybody already has stops working. */}
        {changing ? (
          <Alert tone="note">
            Changing this moves every page you have published. Links to the old
            address will stop working.
          </Alert>
        ) : null}

        {set.error ? <Alert>{set.error.message}</Alert> : null}
      </div>
    </Modal>
  );
}
