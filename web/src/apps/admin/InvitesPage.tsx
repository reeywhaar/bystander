import { useState } from "react";

import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute, until } from "@app/lib/time";
import { useInvites, useRemoveInvite } from "@app/queries/hooks";

import { InviteDialog } from "@app/apps/admin/InviteDialog";

export function InvitesPage() {
  const invites = useInvites();
  const remove = useRemoveInvite();
  const [making, setMaking] = useState(false);

  if (invites.isPending) return <Spinner />;
  if (invites.error) throw invites.error;

  return (
    <div className="flex flex-col gap-8">
      {/* One button, and everything else behind it. What kind of account and how it reaches
          them are two questions whose answers change the form — an address field appears, the
          button changes what it does, and afterwards there is either a link to copy or a
          message to read. Unfolding all of that above the table moves the table while somebody
          is reading it. */}
      <div>
        <Button variant="primary" onClick={() => setMaking(true)}>
          Create invitation
        </Button>
      </div>

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
                <th className="py-2 pr-4 font-medium">Sent to</th>
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
                      {invite.email || (
                        <span className="text-ink-faint">
                          a link, handed over
                        </span>
                      )}
                    </td>
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

      <InviteDialog open={making} onClose={() => setMaking(false)} />
    </div>
  );
}
