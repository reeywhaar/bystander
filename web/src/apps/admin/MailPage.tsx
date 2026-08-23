import { useEffect, useState } from "react";

import type { SmtpTls } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Spinner } from "@app/components/ui/Spinner";
import { since } from "@app/lib/time";
import {
  useForgetSmtp,
  useSaveSmtp,
  useSmtp,
  useTestSmtp,
} from "@app/queries/hooks";

/** The two ports worth pre-filling, and which protection each of them implies. */
const PORTS: Record<SmtpTls, number> = { starttls: 587, implicit: 465 };

export function MailPage() {
  const config = useSmtp();
  const save = useSaveSmtp();
  const forget = useForgetSmtp();
  const test = useTestSmtp();

  const [host, setHost] = useState("");
  const [port, setPort] = useState(587);
  const [tls, setTls] = useState<SmtpTls>("starttls");
  const [username, setUsername] = useState("");
  // Never filled from the server, because the server never sends it. Left empty, saving
  // keeps whatever is already stored.
  const [password, setPassword] = useState("");
  const [from, setFrom] = useState("");
  const [senderName, setSenderName] = useState("");
  const [to, setTo] = useState("");

  const loaded = config.data;
  useEffect(() => {
    if (!loaded) return;
    setHost(loaded.host);
    setPort(loaded.port);
    setTls(loaded.tls);
    setUsername(loaded.username);
    setFrom(loaded.from_address);
    setSenderName(loaded.sender_name);
  }, [loaded]);

  if (config.isPending) return <Spinner />;
  if (config.error) throw config.error;

  const configured = config.data?.configured ?? false;
  // The first save is the only one that must carry a password; later ones may leave the
  // field alone, and an empty box then means "the one already stored".
  const complete =
    host.trim() !== "" &&
    username.trim() !== "" &&
    from.trim() !== "" &&
    (configured || password !== "");

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-2">
        <h2 className="font-serif text-xl text-ink">The relay</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          bystander does not deliver mail itself. It hands each message to a
          relay you already have — your mail provider, or a sending service —
          and that relay does the rest. Until one is configured here, an account
          with a forgotten password is an account nobody can get back into.
        </p>
      </section>

      {/* Bottom-aligned, because a hint makes its field taller and two fields side by
          side then have their boxes at different heights — which reads as a mistake
          rather than as an explanation. */}
      <section className="grid items-end gap-4 sm:grid-cols-2">
        <Field
          label="Host"
          placeholder="smtp.example.com"
          value={host}
          onChange={(event) => setHost(event.target.value)}
        />
        <div className="grid grid-cols-[1fr_auto] gap-3">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="smtp-tls" className="text-sm font-medium text-ink">
              Encryption
            </label>
            <select
              id="smtp-tls"
              value={tls}
              onChange={(event) => {
                const next = event.target.value as SmtpTls;
                setTls(next);
                // Changing the mode moves the port to the one that mode is usually on.
                // Somebody who has typed a port of their own has typed something that is
                // neither default, and that is left alone.
                if (port === PORTS.starttls || port === PORTS.implicit) {
                  setPort(PORTS[next]);
                }
              }}
              className="rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm text-ink"
            >
              <option value="starttls">STARTTLS</option>
              <option value="implicit">TLS from the start</option>
            </select>
          </div>
          <Field
            label="Port"
            type="number"
            min={1}
            max={65535}
            value={port}
            onChange={(event) => setPort(Number(event.target.value))}
            className="w-24"
          />
        </div>

        <Field
          label="Username"
          autoComplete="off"
          value={username}
          onChange={(event) => setUsername(event.target.value)}
        />
        <Field
          label="Password"
          type="password"
          autoComplete="new-password"
          placeholder={configured ? "unchanged" : ""}
          hint={configured ? "Stored. Leave this empty to keep it." : undefined}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />

        <Field
          label="From"
          placeholder="paper@example.com"
          hint="What recipients see. Relays authenticate an account and send as an address, and the two are routinely different."
          value={from}
          onChange={(event) => setFrom(event.target.value)}
        />
        <Field
          label="Sender name"
          placeholder="bystander"
          hint="Optional, shown beside the address."
          value={senderName}
          onChange={(event) => setSenderName(event.target.value)}
        />
      </section>

      {save.error ? <Alert>{save.error.message}</Alert> : null}

      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="primary"
          disabled={!complete || save.isPending}
          onClick={() =>
            save.mutate(
              {
                host,
                port,
                tls,
                username,
                password,
                from_address: from,
                sender_name: senderName,
              },
              // Clearing it afterwards is the point: it is stored now, and keeping it in
              // a form field is keeping it for no reason.
              { onSuccess: () => setPassword("") },
            )
          }
        >
          {save.isPending ? "Saving…" : "Save"}
        </Button>
        {configured ? (
          <span className="text-xs text-ink-faint">
            Last changed {since(config.data?.updated_at ?? 0)}.
          </span>
        ) : null}
      </div>

      {configured ? (
        <section className="flex flex-col gap-3 border-t border-rule pt-8">
          <h2 className="font-serif text-xl text-ink">Send a test</h2>
          <p className="max-w-prose text-sm text-ink-muted">
            One real message, sent while you wait. Connecting is not the same as
            sending — a relay will accept a login and then refuse the From
            address — so this does the whole thing and tells you what came back.
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
              onClick={() => test.mutate(to.trim())}
            >
              {test.isPending ? "Sending…" : "Send"}
            </Button>
          </div>
          {test.error ? <Alert>{test.error.message}</Alert> : null}
          {test.isSuccess ? (
            <p className="text-sm text-ink-muted">
              The relay accepted it. Whether it arrives is now between the relay
              and the recipient.
            </p>
          ) : null}
        </section>
      ) : null}

      {configured ? (
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
                forget.mutate(undefined, {
                  onSuccess: () => {
                    setPassword("");
                    test.reset();
                  },
                })
              }
            >
              {forget.isPending ? "Forgetting…" : "Forget the relay"}
            </Button>
          </div>
        </section>
      ) : null}
    </div>
  );
}
