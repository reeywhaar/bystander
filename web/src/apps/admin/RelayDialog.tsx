import { useState } from "react";

import type { SmtpConfig, SmtpForm, SmtpTls } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { useSaveSmtp, useTestSmtp } from "@app/queries/hooks";

/** What recipients see when no sender name is chosen. Mirrors `mail.DefaultSenderName`. */
const DEFAULT_SENDER_NAME = "bystander";

/** The two ports worth pre-filling, and which protection each of them implies. */
const PORTS: Record<SmtpTls, number> = { starttls: 587, implicit: 465 };

/**
 * The relay, as it is set up or changed.
 *
 * Nothing here is written until Save, and the test button sends with what is typed rather
 * than with what is stored. That is the whole reason this is a dialog: a relay can be tried
 * before it replaces one that already works, so a mistyped password is a message that did
 * not arrive rather than an instance that has quietly stopped being able to send.
 */
export function RelayDialog({
  current,
  onClose,
}: {
  /** What is configured now, or null the first time. */
  current: SmtpConfig | null;
  onClose: () => void;
}) {
  const save = useSaveSmtp();
  const test = useTestSmtp();

  // Mounted only while it is open — see the call site — so every visit starts from what is
  // configured, and a test that failed last time is not still on screen the next time.
  const [state, setState] = useState(() => initial(current));

  const draft: SmtpForm = {
    host: state.host.trim(),
    port: state.port,
    tls: state.tls,
    username: state.username.trim(),
    password: state.password,
    from_address: state.from.trim(),
    sender_name: state.senderName.trim(),
  };

  const set = <K extends keyof typeof state>(
    field: K,
    value: (typeof state)[K],
  ) => setState((was) => ({ ...was, [field]: value }));

  // The first save is the only one that must carry a password; later ones may leave the
  // field alone, and an empty box then means the one already stored.
  const complete =
    draft.host !== "" &&
    draft.username !== "" &&
    draft.from_address !== "" &&
    (current !== null || draft.password !== "");

  return (
    <Modal
      open
      onClose={onClose}
      title={current === null ? "Set up a relay" : "Change the relay"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!complete || save.isPending}
            onClick={() => save.mutate(draft, { onSuccess: onClose })}
          >
            {save.isPending ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field
          label="Host"
          placeholder="smtp.example.com"
          autoFocus
          value={state.host}
          onChange={(event) => set("host", event.target.value)}
        />

        <div className="grid grid-cols-[1fr_5.5rem] items-end gap-3">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="relay-tls" className="text-sm font-medium text-ink">
              Encryption
            </label>
            <select
              id="relay-tls"
              value={state.tls}
              onChange={(event) => {
                const tls = event.target.value as SmtpTls;
                // Changing the mode moves the port to the one that mode is usually on.
                // A port somebody typed themselves is neither default, and is left alone.
                const port =
                  state.port === PORTS.starttls || state.port === PORTS.implicit
                    ? PORTS[tls]
                    : state.port;
                setState((was) => ({ ...was, tls, port }));
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
            value={state.port}
            onChange={(event) => set("port", Number(event.target.value))}
          />
        </div>

        <Field
          label="Username"
          autoComplete="off"
          value={state.username}
          onChange={(event) => set("username", event.target.value)}
        />
        <Field
          label="Password"
          type="password"
          autoComplete="new-password"
          placeholder={current ? "unchanged" : ""}
          hint={
            current
              ? "Stored. Leave this empty to keep the one already set."
              : undefined
          }
          value={state.password}
          onChange={(event) => set("password", event.target.value)}
        />
        <Field
          label="From"
          placeholder="paper@example.com"
          hint="What recipients see. Relays authenticate an account and send as an address, and the two are routinely different."
          value={state.from}
          onChange={(event) => set("from", event.target.value)}
        />
        <Field
          label="Sender name"
          placeholder={DEFAULT_SENDER_NAME}
          hint={`Optional. Left empty, recipients see ${DEFAULT_SENDER_NAME}.`}
          value={state.senderName}
          onChange={(event) => set("senderName", event.target.value)}
        />

        {/* Tried before it is saved, which is the whole point of this being a dialog:
            otherwise the only way to find out whether a password is right is to commit it,
            and the configuration it replaced is gone by then. */}
        <section className="flex flex-col gap-2 border-t border-rule pt-4">
          <div className="flex items-end gap-2">
            <Field
              label="Try it first"
              type="email"
              placeholder="you@example.com"
              value={state.to}
              onChange={(event) => set("to", event.target.value)}
              className="min-w-0 flex-1"
            />
            <Button
              className="shrink-0"
              disabled={!complete || state.to.trim() === "" || test.isPending}
              onClick={() => test.mutate({ to: state.to.trim(), relay: draft })}
            >
              {test.isPending ? "Sending…" : "Send a test"}
            </Button>
          </div>
          <p className="text-xs text-ink-muted">
            Sends one message with what is typed above. Nothing is saved.
          </p>
          {test.error ? <Alert>{test.error.message}</Alert> : null}
          {test.isSuccess ? (
            <Alert tone="note">The relay accepted it.</Alert>
          ) : null}
        </section>

        {save.error ? <Alert>{save.error.message}</Alert> : null}
      </div>
    </Modal>
  );
}

/**
 * The form as it opens: what is configured, except the password.
 *
 * The password is never sent to the browser, so a filled-looking field would be a lie about
 * what saving would do.
 */
function initial(current: SmtpConfig | null) {
  return {
    host: current?.host ?? "",
    port: current?.port ?? PORTS.starttls,
    tls: current?.tls ?? ("starttls" as SmtpTls),
    username: current?.username ?? "",
    password: "",
    from: current?.from_address ?? "",
    senderName: current?.sender_name ?? "",
    to: "",
  };
}
