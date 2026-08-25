import { useState, type FormEvent } from "react";

import type { Role } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { CopyBox } from "@app/components/ui/CopyBox";
import { Field } from "@app/components/ui/Field";
import { Spinner } from "@app/components/ui/Spinner";
import { absolute, until } from "@app/lib/time";
import {
  useCreateInvite,
  useInvites,
  useRemoveInvite,
  useSmtp,
} from "@app/queries/hooks";

/** Hand the link over yourself, or have the instance send it. */
type Delivery = "link" | "email";

export function InvitesPage() {
  const invites = useInvites();
  const create = useCreateInvite();
  const remove = useRemoveInvite();
  // Only for whether a relay exists. The admin island already reads this for the Mail tab, so
  // it is usually cached by the time anybody gets here.
  const smtp = useSmtp();

  const [role, setRole] = useState<Role>("user");
  const [delivery, setDelivery] = useState<Delivery>("link");
  const [email, setEmail] = useState("");
  // The link the server just returned. Held here rather than read back from the listing,
  // because the listing does not carry it: the token is stored as a hash and this is the
  // only moment it is ever readable.
  const [minted, setMinted] = useState<string | null>(null);
  // The address the last one went to, so "it has been sent" is a sentence rather than a
  // silence. There is nothing else to show: the link deliberately does not come back.
  const [sent, setSent] = useState<string | null>(null);

  function submit(event: FormEvent) {
    event.preventDefault();
    setMinted(null);
    setSent(null);
    create.mutate(
      { role, email: delivery === "email" ? email : "" },
      {
        onSuccess: (invite) => {
          if (invite.email) {
            setSent(invite.email);
            setEmail("");
            return;
          }
          setMinted(invite.url ?? null);
        },
      },
    );
  }

  if (invites.isPending) return <Spinner />;
  if (invites.error) throw invites.error;

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-3">
        <form onSubmit={submit} className="flex flex-col gap-3">
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

            {/* Chosen rather than inferred from whether the address field is filled in. The two
                deliveries differ in more than convenience — an emailed link is never shown
                here, because that is what makes accepting it prove the address — so which one
                is happening should be a decision somebody made rather than a side effect of a
                field they typed in. */}
            <select
              value={delivery}
              onChange={(event) => setDelivery(event.target.value as Delivery)}
              aria-label="How the invitation reaches them"
              className="rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm text-ink"
            >
              <option value="link">give me a link to pass on</option>
              <option value="email">send it to an address</option>
            </select>

            {delivery === "link" ? (
              <Button
                variant="primary"
                type="submit"
                disabled={create.isPending}
              >
                {create.isPending ? "Minting…" : "Mint a link"}
              </Button>
            ) : null}
          </div>

          {/* The gate. Reached by choosing to send rather than by finding the control dead:
              "send it to an address" is a reasonable thing to want, and a greyed button that
              says nothing leaves somebody looking for the reason in the wrong place. */}
          {delivery === "email" && smtp.data && !smtp.data.configured ? (
            <Alert tone="note">
              No mail relay is configured, so this instance cannot send anything
              yet. Set one up under{" "}
              <a href="/admin/mail" className="underline underline-offset-2">
                Mail
              </a>
              , or mint a link and pass it on yourself.
            </Alert>
          ) : null}

          {delivery === "email" ? (
            <div className="flex flex-wrap items-end gap-2">
              <Field
                label="Send it to"
                type="email"
                required
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                placeholder="them@example.com"
                disabled={!smtp.data?.configured}
                className="min-w-64 flex-1"
                hint="This becomes the account's recovery address when they accept, because the invitation reached them at it. The link is not shown here — that is what makes it proof."
              />
              <Button
                variant="primary"
                type="submit"
                disabled={create.isPending || !smtp.data?.configured}
              >
                {create.isPending ? "Sending…" : "Send it"}
              </Button>
            </div>
          ) : null}
        </form>

        {create.error ? <Alert>{create.error.message}</Alert> : null}

        {sent ? (
          <p className="text-sm text-ink-muted">
            Sent to {sent}. It works once, and lapses in a week.
          </p>
        ) : null}

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
    </div>
  );
}
