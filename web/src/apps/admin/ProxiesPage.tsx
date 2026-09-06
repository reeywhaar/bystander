import { useState } from "react";

import type { Proxy } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { Spinner } from "@app/components/ui/Spinner";
import { since } from "@app/lib/time";
import {
  useForgetProxy,
  useProxies,
  useResetProxy,
  useSaveProxy,
} from "@app/queries/hooks";

import { ProxyDialog } from "@app/apps/admin/ProxyDialog";
import { ProxyTestDialog } from "@app/apps/admin/ProxyTestDialog";

/** What each kind is called where somebody reads it rather than where it is stored. */
const KIND_NAME: Record<string, string> = {
  proxio: "proxio",
  socks5: "SOCKS5",
};

/** Where proxio comes from, for somebody who has not met it. */
const PROXIO_URL = "https://github.com/reeywhaar/proxio";

function ProxioLink() {
  return (
    <a
      href={PROXIO_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="underline underline-offset-2 hover:text-ink"
    >
      proxio
    </a>
  );
}

/**
 * Relays a publisher that refuses this instance can be reached through.
 *
 * The page lists what is configured; adding and changing happen in a dialog, which is what
 * makes it possible to try a relay before saving it. Editing in place would mean the only way
 * to find out whether a token works is to commit it.
 */
export function ProxiesPage() {
  const proxies = useProxies();
  const [editing, setEditing] = useState<Proxy | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<Proxy | null>(null);

  if (proxies.isPending) return <Spinner />;
  if (proxies.error) throw proxies.error;

  const list = proxies.data?.proxies ?? [];

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-2">
        <h2 className="font-serif text-xl text-ink">Relays</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Some publishers refuse this instance — geo-fenced, behind a bot wall,
          or rate-limiting the address it fetches from. A relay is somewhere
          else to ask from, and it has to be genuinely somewhere else: one on
          this network goes out from this address and gets refused in exactly
          the same way. Each feed is still tried directly first; only when that
          is refused are these used, in the order below.
        </p>
        <p className="max-w-prose text-sm text-ink-muted">
          Whichever relay reaches a publisher is remembered, so the feed and its
          pictures go straight there next time instead of being refused again
          first. That is kept until the relay stops working — a region a feed is
          not licensed in is not a thing that lapses. Reset forgets what one
          relay has learned, for when you know something this does not.
        </p>
      </section>

      {list.length === 0 ? (
        <section className="flex flex-col gap-4">
          <p className="max-w-prose text-sm text-ink-muted">
            None configured. Every publisher is reached directly or not at all.
          </p>
          {/* What to go and get, for somebody who has decided they want one. Two answers,
              because one of them is probably already lying around. */}
          <p className="max-w-prose text-sm text-ink-muted">
            Two kinds work here. <ProxioLink /> is a small HTTP relay you run
            yourself. Or point this at any SOCKS5 endpoint you already have — a
            tunnel, or something a hosting provider gives you.
          </p>
          <div>
            <Button variant="primary" onClick={() => setEditing(null)}>
              Add a relay
            </Button>
          </div>
        </section>
      ) : (
        <>
          <ul className="flex flex-col gap-3">
            {list.map((proxy) => (
              <ProxyRow
                key={proxy.id}
                proxy={proxy}
                onEdit={() => setEditing(proxy)}
                onDelete={() => setDeleting(proxy)}
              />
            ))}
          </ul>
          <div>
            <Button onClick={() => setEditing(null)}>Add another</Button>
          </div>
        </>
      )}

      {/* Mounted only while open, so each visit starts from what is stored rather than
          from whatever was typed last time. */}
      {editing !== undefined ? (
        <ProxyDialog
          current={editing}
          onClose={() => setEditing(undefined)}
        />
      ) : null}

      <DeleteProxyDialog proxy={deleting} onClose={() => setDeleting(null)} />
    </div>
  );
}

/** One relay: what it is, whether it is in use, and what can be done about it. */
function ProxyRow({
  proxy,
  onEdit,
  onDelete,
}: {
  proxy: Proxy;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const save = useSaveProxy();
  const reset = useResetProxy();
  const [trying, setTrying] = useState(false);

  // Switching one off writes the whole entry back, which is what the endpoint takes. The
  // token is left empty on purpose: empty means the one already stored, so parking a relay
  // does not require knowing its credential.
  const toggle = () =>
    save.mutate({
      id: proxy.id,
      form: {
        kind: proxy.kind,
        label: proxy.label,
        url: proxy.url,
        username: proxy.username,
        token: "",
        priority: proxy.priority,
        enabled: !proxy.enabled,
      },
    });

  return (
    <li className="flex flex-col gap-2 rounded-md border border-rule bg-paper-raised p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-ink">
            {proxy.label || proxy.url}
            {proxy.enabled ? null : (
              <span className="ml-2 text-xs text-ink-faint">· off</span>
            )}
          </p>
          {/* The address only when it is not already the name above. A relay with no label
              is named by its address, and printing it twice reads as a mistake. */}
          <p className="truncate text-sm text-ink-muted">
            {KIND_NAME[proxy.kind] ?? proxy.kind}
            {proxy.label ? ` · ${proxy.url}` : ""}
            {proxy.username ? ` · ${proxy.username}` : ""}
          </p>
          <p className="text-xs text-ink-faint">
            priority {proxy.priority} · changed {since(proxy.updated_at)}
            {proxy.routes > 0
              ? ` · ${proxy.routes} publisher${proxy.routes === 1 ? "" : "s"} reached through it`
              : ""}
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <Button onClick={() => setTrying(true)}>Try it</Button>
          <Button disabled={save.isPending} onClick={toggle}>
            {proxy.enabled ? "Switch off" : "Switch on"}
          </Button>
          {/* Only where there is something to forget. A button that always does nothing
              is a button somebody has to press to find that out. */}
          {proxy.routes > 0 ? (
            <Button
              disabled={reset.isPending}
              onClick={() => reset.mutate(proxy.id)}
            >
              {reset.isPending ? "Forgetting…" : "Reset"}
            </Button>
          ) : null}
          <Button onClick={onEdit}>Change</Button>
          <Button variant="danger" onClick={onDelete}>
            Delete
          </Button>
        </div>
      </div>

      {save.error ? <Alert>{save.error.message}</Alert> : null}
      {reset.error ? <Alert>{reset.error.message}</Alert> : null}
      {reset.isSuccess ? (
        <Alert tone="note">
          Forgotten. Each of those publishers is tried directly again on its
          next fetch, and learns its way back if it is still refused.
        </Alert>
      ) : null}

      {/* Mounted only while open, so each visit starts from an empty address rather than
          from whatever was asked about last time. */}
      {trying ? (
        <ProxyTestDialog
          name={proxy.label || proxy.url}
          id={proxy.id}
          onClose={() => setTrying(false)}
        />
      ) : null}
    </li>
  );
}

/**
 * Deleting a relay, and what goes with it.
 *
 * The token goes with it, which is the part worth asking about: switching a relay off keeps
 * the credential so it can be switched back on, and deleting does not. Anything currently
 * reached through it falls back to being refused directly.
 */
function DeleteProxyDialog({
  proxy,
  onClose,
}: {
  proxy: Proxy | null;
  onClose: () => void;
}) {
  const remove = useForgetProxy();
  if (!proxy) return null;

  return (
    <Modal
      open
      onClose={onClose}
      title={`Delete ${proxy.label || proxy.url}?`}
      footer={
        <>
          <Button onClick={onClose} disabled={remove.isPending}>
            Keep it
          </Button>
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(proxy.id, { onSuccess: onClose })}
          >
            {remove.isPending ? "Deleting…" : "Delete it"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-sm text-ink-muted">
          The {proxy.kind === "socks5" ? "password" : "token"} goes with it, and
          it is not shown anywhere, so putting this relay back means finding the
          credential again. To stop using it for now, switch it off instead —
          that keeps everything.
        </p>
        <p className="text-sm text-ink-muted">
          {proxy.routes > 0
            ? `The ${proxy.routes} publisher${proxy.routes === 1 ? "" : "s"} reached through it go back to being refused, unless another relay can get to them.`
            : "Nothing is currently reached through it."}
        </p>
        {remove.error ? <Alert>{remove.error.message}</Alert> : null}
      </div>
    </Modal>
  );
}
