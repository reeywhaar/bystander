import { useState } from "react";

import type { Role } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { CopyBox } from "@app/components/ui/CopyBox";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute, until } from "@app/lib/time";
import {
  useCreateInvite,
  useInvites,
  useRemoveInvite,
} from "@app/queries/hooks";

export function InvitesPage() {
  const invites = useInvites();
  const create = useCreateInvite();
  const remove = useRemoveInvite();

  const [role, setRole] = useState<Role>("user");
  // The link the server just returned. Held here rather than read back from the listing,
  // because the listing does not carry it: the token is stored as a hash and this is the
  // only moment it is ever readable.
  const [minted, setMinted] = useState<string | null>(null);

  if (invites.isPending) return <Spinner />;
  if (invites.error) throw invites.error;

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={role}
            onChange={(event) => setRole(event.target.value as Role)}
            aria-label="What kind of account this invitation makes"
            className="rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm text-ink"
          >
            <option value="user">an ordinary account</option>
            <option value="admin">an administrator</option>
          </select>
          <Button
            variant="primary"
            disabled={create.isPending}
            onClick={() =>
              create.mutate(role, {
                onSuccess: (invite) => setMinted(invite.url ?? null),
              })
            }
          >
            {create.isPending ? "Minting…" : "Mint a link"}
          </Button>
        </div>

        {create.error ? <Alert>{create.error.message}</Alert> : null}

        {minted ? (
          <div className="flex flex-col gap-2">
            <CopyBox value={minted} shareTitle="An invitation to bystander" />
            <p className="text-xs text-ink-muted">
              Take this now — it is the only time it can be read. What is stored
              is a hash, so a lost link is replaced rather than recovered.
            </p>
          </div>
        ) : null}
      </section>

      <section>
        {invites.data.length === 0 ? (
          <p className="py-10 text-center text-sm text-ink-muted">
            No invitations yet.
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-rule text-left text-xs tracking-wide text-ink-faint uppercase">
                <th className="py-2 pr-4 font-medium">For</th>
                <th className="py-2 pr-4 font-medium">State</th>
                <th className="py-2 pr-4 font-medium">Made</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {invites.data.map((invite) => {
                const accepted = invite.accepted_at !== null;
                const expired =
                  !accepted && invite.expires_at * 1000 < Date.now();
                return (
                  <tr key={invite.id} className="border-b border-rule">
                    <td className="py-3 pr-4 text-ink">{invite.role}</td>
                    <td className="py-3 pr-4 text-ink-muted">
                      {accepted ? (
                        <>became {invite.username || "an account"}</>
                      ) : expired ? (
                        <span className="text-ink-faint">expired</span>
                      ) : (
                        <>outstanding, lapses {until(invite.expires_at)}</>
                      )}
                    </td>
                    <td className="py-3 pr-4 text-ink-muted">
                      {absolute(invite.created_at)}
                    </td>
                    <td className="py-3 text-right">
                      {/* An accepted invitation is the record of where an account came
                          from, so the server refuses to delete it. */}
                      {accepted ? null : (
                        <button
                          type="button"
                          onClick={() => remove.mutate(invite.id)}
                          className="text-xs text-ink-faint hover:text-accent"
                        >
                          Withdraw
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {remove.error ? (
          <div className="mt-3">
            <Alert>{remove.error.message}</Alert>
          </div>
        ) : null}
      </section>
    </div>
  );
}
