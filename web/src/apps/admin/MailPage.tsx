import { useState } from "react";

import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Spinner } from "@app/components/ui/Spinner";
import { since } from "@app/lib/time";
import { useForgetSmtp, useSmtp, useTestSmtp } from "@app/queries/hooks";

import { RelayDialog } from "@app/apps/admin/RelayDialog";

/** What recipients see when no sender name is chosen. Mirrors `mail.DefaultSenderName`. */
const DEFAULT_SENDER_NAME = "bystander";

/**
 * Where mail goes out through.
 *
 * The page states what is configured; changing it happens in a dialog, which is what makes
 * it possible to try a relay before saving it. Editing in place would mean the only way to
 * find out whether a password works is to commit it — and by then the configuration it
 * replaced is gone.
 */
export function MailPage() {
  const config = useSmtp();
  const forget = useForgetSmtp();
  const test = useTestSmtp();

  const [editing, setEditing] = useState(false);
  const [to, setTo] = useState("");

  if (config.isPending) return <Spinner />;
  if (config.error) throw config.error;

  const relay = config.data?.configured ? config.data : null;

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-2">
        <h2 className="font-serif text-xl text-ink">The relay</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          bystander does not deliver mail itself. It hands each message to a
          relay you already have — your mail provider, or a sending service —
          and that relay does the rest. Until one is configured, an account with
          a forgotten password is an account nobody can get back into.
        </p>
      </section>

      {relay === null ? (
        <section className="flex flex-wrap items-center gap-4">
          <p className="text-sm text-ink-muted">
            Nothing is configured, so nothing is sent.
          </p>
          <Button variant="primary" onClick={() => setEditing(true)}>
            Set up a relay
          </Button>
        </section>
      ) : (
        <>
          {/* A description list rather than a form: this is what is true, and changing it
              is somewhere else. */}
          <section className="flex flex-wrap items-start justify-between gap-4">
            <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm">
              <dt className="text-ink-muted">Relay</dt>
              <dd className="text-ink">
                {relay.host}:{relay.port}{" "}
                <span className="text-ink-faint">
                  · {relay.tls === "implicit" ? "TLS" : "STARTTLS"}
                </span>
              </dd>
              <dt className="text-ink-muted">Sends as</dt>
              <dd className="text-ink">
                {relay.sender_name || DEFAULT_SENDER_NAME}{" "}
                <span className="text-ink-faint">
                  &lt;{relay.from_address}&gt;
                </span>
              </dd>
              <dt className="text-ink-muted">Signs in as</dt>
              <dd className="text-ink">{relay.username}</dd>
              <dt className="text-ink-muted">Last changed</dt>
              <dd className="text-ink">{since(relay.updated_at)}</dd>
            </dl>
            <Button onClick={() => setEditing(true)}>Change</Button>
          </section>

          <section className="flex flex-col gap-3 border-t border-rule pt-8">
            <h2 className="font-serif text-xl text-ink">Send a test</h2>
            <p className="max-w-prose text-sm text-ink-muted">
              One real message through the relay above, sent while you wait.
              Connecting is not the same as sending — a relay will accept a
              login and then refuse the From address — so this does the whole
              thing and tells you what came back.
            </p>
            <div className="flex flex-wrap items-end gap-3">
              <Field
                label="To"
                type="email"
                placeholder="you@example.com"
                value={to}
                onChange={(event) => setTo(event.target.value)}
                className="min-w-64 flex-1"
              />
              <Button
                disabled={to.trim() === "" || test.isPending}
                onClick={() => test.mutate({ to: to.trim() })}
              >
                {test.isPending ? "Sending…" : "Send"}
              </Button>
            </div>
            {test.error ? <Alert>{test.error.message}</Alert> : null}
            {test.isSuccess ? (
              <Alert tone="note">
                The relay accepted it. Whether it arrives is now between the
                relay and the recipient.
              </Alert>
            ) : null}
          </section>

          <section className="flex flex-col gap-3 border-t border-rule pt-8">
            <h2 className="font-serif text-xl text-ink">Forget it</h2>
            <p className="max-w-prose text-sm text-ink-muted">
              Removes the relay and the password with it. Nothing will be sent
              afterwards — it will be refused rather than attempted quietly.
            </p>
            {forget.error ? <Alert>{forget.error.message}</Alert> : null}
            <div>
              <Button
                variant="danger"
                disabled={forget.isPending}
                onClick={() =>
                  forget.mutate(undefined, { onSuccess: () => test.reset() })
                }
              >
                {forget.isPending ? "Forgetting…" : "Forget the relay"}
              </Button>
            </div>
          </section>
        </>
      )}

      {/* Mounted only while open, so it opens on what is configured every time rather than
          on whatever was typed and abandoned last time. */}
      {editing ? (
        <RelayDialog current={relay} onClose={() => setEditing(false)} />
      ) : null}
    </div>
  );
}
