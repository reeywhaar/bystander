import { useState } from "react";

import type { ProxyForm } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { useTestProxy } from "@app/queries/hooks";

/**
 * Asking a relay to fetch something, and seeing what came back.
 *
 * The address is asked for rather than assumed, because the useful test is rarely "does this
 * relay work at all" — it is "does this relay get me past the thing that refused us". Somebody
 * setting one up has a feed in mind, and typing it here answers the question they actually
 * have. Left empty it fetches this instance's own address, which answers the smaller question.
 *
 * Opened from two places, and it has to work in both: from a row, for a relay that is stored,
 * and from inside the dialog that is editing or adding one, for settings that may never have
 * been saved. That second case is the whole reason the endpoint takes a relay in its body — the
 * only way to find out whether a token works should not be to save it over one that already did.
 */
export function ProxyTestDialog({
  name,
  id,
  proxy,
  onClose,
}: {
  /** What to call the relay in the title. */
  name: string;
  /** The stored relay, when there is one. */
  id?: string;
  /** Settings to try instead of what is stored, when they differ or nothing is stored yet. */
  proxy?: ProxyForm;
  onClose: () => void;
}) {
  const test = useTestProxy();
  const [url, setUrl] = useState("");

  const target = url.trim();
  const result = test.data;

  return (
    <Modal
      open
      onClose={onClose}
      title={`Try ${name}`}
      footer={
        <>
          <Button onClick={onClose}>Close</Button>
          <Button
            variant="primary"
            disabled={test.isPending}
            onClick={() =>
              test.mutate({ id, proxy, url: target === "" ? undefined : target })
            }
          >
            {test.isPending ? "Trying…" : "Try it"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field
          label="Address to fetch"
          placeholder="https://www.example.com/feed"
          autoFocus
          hint="A feed that has been refusing us is the useful thing to type here. Left empty, it fetches this instance's own address, which only says the relay works at all."
          value={url}
          onChange={(event) => setUrl(event.target.value)}
        />

        <p className="text-xs text-ink-muted">
          Nothing is saved either way, and the request really is made — a relay
          will accept a connection and then refuse a stale token, so anything
          short of fetching would report that one as working.
        </p>

        {test.error ? <Alert>{test.error.message}</Alert> : null}
        {result && !result.ok ? <Alert>{result.error}</Alert> : null}
        {result?.ok ? (
          <Alert tone="note">
            It got through. {result.url} answered {result.status}.
          </Alert>
        ) : null}
      </div>
    </Modal>
  );
}
