import { useState } from "react";

import type { Me, User } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute } from "@app/lib/time";
import {
  useRemoveUser,
  useSetUserDisabled,
  useUsers,
} from "@app/queries/hooks";

import { RecoveryDialog } from "@app/apps/admin/RecoveryDialog";

export function UsersPage({ me }: { me: Me }) {
  const users = useUsers();
  const setDisabled = useSetUserDisabled();
  const remove = useRemoveUser();
  // Which account a link is being minted for, or null. The account rather than the id,
  // because the dialog says whose it is and would otherwise have to look that up again.
  const [recovering, setRecovering] = useState<User | null>(null);

  if (users.isPending) return <Spinner />;
  if (users.error) throw users.error;

  return (
    <div className="flex flex-col gap-4">
      {setDisabled.error ? <Alert>{setDisabled.error.message}</Alert> : null}
      {remove.error ? <Alert>{remove.error.message}</Alert> : null}

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-rule text-left text-xs tracking-wide text-ink-faint uppercase">
              <th className="py-2 pr-4 font-medium">Name</th>
              <th className="py-2 pr-4 font-medium">Role</th>
              <th className="py-2 pr-4 font-medium">Feeds</th>
              <th className="py-2 pr-4 font-medium">Since</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody>
            {users.data.map((user) => {
              const self = user.id === me.id;
              const disabled = user.disabled_at !== null;
              return (
                <tr
                  key={user.id}
                  className={`border-b border-rule ${disabled ? "opacity-50" : ""}`}
                >
                  <td className="py-3 pr-4">
                    <span className="font-serif text-base text-ink">
                      {user.username}
                    </span>
                    {self ? (
                      <span className="ml-2 text-xs text-ink-faint">you</span>
                    ) : null}
                    {disabled ? (
                      <span className="ml-2 text-xs text-accent">disabled</span>
                    ) : null}
                  </td>
                  <td className="py-3 pr-4 text-ink-muted">{user.role}</td>
                  <td className="py-3 pr-4 tabular-nums text-ink-muted">
                    {user.feed_count}
                  </td>
                  <td className="py-3 pr-4 text-ink-muted">
                    {absolute(user.created_at)}
                  </td>
                  <td className="py-3 text-right">
                    <span className="flex flex-wrap justify-end gap-3">
                      {/* Not for a disabled account: a new password does not let anybody
                          into one that is switched off, so the server refuses to mint a
                          link at all. Enable it first. */}
                      {disabled ? null : (
                        <button
                          type="button"
                          onClick={() => setRecovering(user)}
                          className="text-xs text-ink-faint hover:text-ink"
                        >
                          Recovery link
                        </button>
                      )}
                      {/* Neither of the other two is offered for your own account. The
                          server refuses both outright — this only keeps the button from
                          being there to press. */}
                      {self ? null : (
                        <>
                          <button
                            type="button"
                            onClick={() =>
                              setDisabled.mutate({
                                id: user.id,
                                disabled: !disabled,
                              })
                            }
                            className="text-xs text-ink-faint hover:text-ink"
                          >
                            {disabled ? "Enable" : "Disable"}
                          </button>
                          <button
                            type="button"
                            onClick={() => remove.mutate(user.id)}
                            className="text-xs text-ink-faint hover:text-accent"
                          >
                            Delete
                          </button>
                        </>
                      )}
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <p className="text-xs text-ink-muted">
        Disabling ends that account's sessions at once and keeps its feeds, so
        enabling it again finds everything where it was left. Deleting does not.
        A recovery link is a way back in for somebody who has lost their
        password; making one changes nothing until it is used.
      </p>

      <RecoveryDialog user={recovering} onClose={() => setRecovering(null)} />
    </div>
  );
}
