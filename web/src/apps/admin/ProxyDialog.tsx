import { useState, type ReactNode } from "react";

import type { Proxy, ProxyForm, ProxyKind } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { Select } from "@app/components/ui/Select";
import { Slider } from "@app/components/ui/Slider";
import { useSaveProxy } from "@app/queries/hooks";

import { ProxyTestDialog } from "@app/apps/admin/ProxyTestDialog";

/** Where proxio comes from, for somebody who has not met it. */
const PROXIO_URL = "https://github.com/reeywhaar/proxio";

/**
 * What each kind is, for somebody choosing between them.
 *
 * Not what it does to a request — that is this program's business and nobody setting one up
 * needs it. What they need is whether they already have one of these, and where to get one if
 * not, which is why proxio's says what it is and links to it.
 */
const ABOUT: Record<ProxyKind, ReactNode> = {
  proxio: (
    <>
      A small HTTP relay you run yourself, somewhere else: it takes a URL and a
      token and fetches the address from there.{" "}
      <a
        href={PROXIO_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="underline underline-offset-2 hover:text-ink"
      >
        github.com/reeywhaar/proxio
      </a>
    </>
  ),
  socks5: (
    <>
      A standard SOCKS5 endpoint — a tunnel you already have, or one a hosting
      provider gives you. Nothing to install here; the request goes out
      unchanged and only the connection is made somewhere else.
    </>
  ),
};

/**
 * What each kind's address looks like.
 *
 * A whole address on somebody else's domain, and that is the point rather than a nicety. The
 * obvious short form — `http://proxio:80`, the service name on a compose network — is not just
 * terse, it is a relay that cannot work: it shares this instance's address, so a publisher
 * refusing us refuses it identically. A placeholder that suggested it would be pointing at the
 * one deployment guaranteed to fail.
 */
const EXAMPLE: Record<ProxyKind, string> = {
  proxio: "https://proxio.example.com",
  socks5: "socks5://socks.example.com:1080",
};

/** What to say under the address box, which differs by kind. */
const ADDRESS_HINT: Record<ProxyKind, string> = {
  proxio: "The host only — the path it serves on is ours to know.",
  socks5: "Host and port. socks5h works too, and means the same thing here.",
};

/** Only one of them authenticates with a name as well as a secret. */
const NEEDS_USERNAME: Record<ProxyKind, boolean> = {
  proxio: false,
  socks5: true,
};

/** What the secret is called, which is not the same word for both. */
const SECRET: Record<ProxyKind, string> = {
  proxio: "Token",
  socks5: "Password",
};

/**
 * One relay, as it is added or changed.
 *
 * Nothing is written until Save, and Test dials with what is typed rather than with what is
 * stored. That is the whole reason this is a dialog: a relay can be tried before it replaces
 * one that already works, so a mistyped token is a test that failed rather than an instance
 * that has quietly stopped being able to reach anything.
 */
export function ProxyDialog({
  current,
  onClose,
}: {
  /** What is stored now, or null when adding one. */
  current: Proxy | null;
  onClose: () => void;
}) {
  const save = useSaveProxy();
  const [trying, setTrying] = useState(false);

  // Mounted only while it is open — see the call site — so every visit starts from what is
  // stored, and a test that failed last time is not still on screen the next time.
  const [state, setState] = useState(() => initial(current));

  const draft: ProxyForm = {
    kind: state.kind,
    label: state.label.trim(),
    url: state.url.trim(),
    username: NEEDS_USERNAME[state.kind] ? state.username.trim() : "",
    token: state.token,
    priority: state.priority,
    enabled: state.enabled,
  };

  const set = <K extends keyof typeof state>(
    field: K,
    value: (typeof state)[K],
  ) => setState((was) => ({ ...was, [field]: value }));

  // The first save is the only one that must carry a token; later ones may leave the field
  // alone, and an empty box then means the one already stored.
  const complete =
    draft.url !== "" &&
    (current !== null || draft.token !== "") &&
    (!NEEDS_USERNAME[draft.kind] || draft.username !== "");

  return (
    <Modal
      open
      onClose={onClose}
      title={current === null ? "Add a relay" : "Change this relay"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!complete || save.isPending}
            onClick={() =>
              save.mutate(
                { id: current?.id, form: draft },
                { onSuccess: onClose },
              )
            }
          >
            {save.isPending ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Select
          label="Kind"
          hint={ABOUT[state.kind]}
          value={state.kind}
          onChange={(event) => {
            const kind = event.target.value as ProxyKind;
            // The address is written differently for each, so an example left over from the
            // other kind would be refused on save. Only one somebody typed is worth keeping.
            const url =
              state.url === "" || state.url === EXAMPLE[state.kind]
                ? ""
                : state.url;
            setState((was) => ({ ...was, kind, url }));
          }}
        >
          <option value="proxio">proxio</option>
          <option value="socks5">SOCKS5</option>
        </Select>

        <Field
          label="Address"
          placeholder={EXAMPLE[state.kind]}
          autoFocus
          hint={ADDRESS_HINT[state.kind]}
          value={state.url}
          onChange={(event) => set("url", event.target.value)}
        />

        {/* The mistake worth heading off. A relay next to this one shares its address, so a
            publisher refusing this instance refuses the relay in exactly the same way — and
            the symptom is a relay that tests fine against anything else and never once helps. */}
        <p className="-mt-2 max-w-prose text-xs text-ink-muted">
          It has to be somewhere this instance is not. A relay on the same
          network goes out from the same address, so a publisher refusing us
          refuses it the same way — the point is reaching them from a place they
          will answer.
        </p>

        {NEEDS_USERNAME[state.kind] ? (
          <Field
            label="Username"
            autoComplete="off"
            value={state.username}
            onChange={(event) => set("username", event.target.value)}
          />
        ) : null}

        <Field
          label={SECRET[state.kind]}
          type="password"
          autoComplete="new-password"
          placeholder={current ? "unchanged" : ""}
          hint={
            current
              ? "Stored. Leave this empty to keep the one already set."
              : undefined
          }
          value={state.token}
          onChange={(event) => set("token", event.target.value)}
        />

        <Field
          label="Name"
          placeholder="optional"
          hint="What to call it in the list. The address does fine on its own."
          value={state.label}
          onChange={(event) => set("label", event.target.value)}
        />

        {/* The same scale and the same direction as a feed's priority, because two numbers in
            one product that both run 0..100 and disagree about which end is which would be a
            small cruelty. Not the same *kind* of number — a feed's is a probability and this
            is an ordering — which is why it does not borrow the feed's words for it. */}
        <div className="flex flex-col gap-1.5">
          <p className="text-xs text-ink-muted">
            Higher is tried first. Zero means tried last rather than never —
            switch a relay off for that.
          </p>
          <Slider
            label="Priority"
            value={state.priority}
            min={0}
            max={100}
            step={5}
            stacked
            className="w-full"
            onCommit={(priority) => set("priority", priority)}
            format={(priority) => (
              <span className="tabular-nums">{priority}</span>
            )}
          />
        </div>

        {/* Dialled before it is saved, which is the whole point of this being a dialog:
            otherwise the only way to find out whether a token is right is to commit it, and
            whatever was there before is gone by then. Works with no id at all, so a relay
            nobody has saved yet can still be tried. */}
        <section className="flex items-center justify-between gap-3 border-t border-rule pt-4">
          <p className="text-sm text-ink-muted">
            Ask it to fetch something, with what is typed above.
          </p>
          <Button
            className="shrink-0"
            disabled={!complete}
            onClick={() => setTrying(true)}
          >
            Try it
          </Button>
        </section>

        {save.error ? <Alert>{save.error.message}</Alert> : null}
      </div>

      {/* A dialog from inside a dialog, which Modal holds the one underneath still for. */}
      {trying ? (
        <ProxyTestDialog
          name={draft.label || draft.url}
          id={current?.id}
          proxy={draft}
          onClose={() => setTrying(false)}
        />
      ) : null}
    </Modal>
  );
}

/**
 * The form as it opens: what is stored, except the token.
 *
 * The token is never sent to the browser, so a filled-looking field would be a lie about what
 * saving would do.
 */
function initial(current: Proxy | null) {
  return {
    kind: current?.kind ?? ("proxio" as ProxyKind),
    label: current?.label ?? "",
    url: current?.url ?? "",
    username: current?.username ?? "",
    token: "",
    // The top of the range when there is nothing to inherit. A relay somebody has just gone
    // to the trouble of configuring is one they want used. Mirrors api.DefaultProxyPriority.
    priority: current?.priority ?? 100,
    enabled: current?.enabled ?? true,
  };
}
