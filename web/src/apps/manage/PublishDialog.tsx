import { useEffect, useState } from "react";

import type { Account, Page } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { Modal } from "@app/components/ui/Modal";
import { usePublishPage } from "@app/queries/hooks";

/** A slug proposed from a name, so nobody has to think about URLs. */
function tidy(typed: string): string {
  return typed
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

/**
 * Puts one page on the open web.
 *
 * Asked once and answered in one gesture: where it lives, and whether a search engine may keep
 * it. The second question is only asked where the instance allows it at all — an administrator's
 * answer is the whole answer, and a control that argues with it would be a control that lies.
 */
export function PublishDialog({
  page,
  account,
  open,
  onClose,
}: {
  page: Page | null;
  account: Account;
  open: boolean;
  onClose: () => void;
}) {
  const publish = usePublishPage();

  const [slug, setSlug] = useState("");
  const [indexable, setIndexable] = useState(false);

  useEffect(() => {
    if (!open || !page) return;
    // The address it was at, if it has been published before — so taking a page down and
    // putting it back offers the address the links already point at.
    setSlug(page.publish_slug || tidy(page.name));
    setIndexable(page.indexable);
  }, [open, page]);

  const proposed = tidy(slug);
  const at = `/p/${account.public_name}/${proposed || "…"}`;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={page ? `Publish ${page.name}` : "Publish"}
      footer={
        <>
          <Button onClick={onClose} disabled={publish.isPending}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={proposed === "" || publish.isPending}
            onClick={() =>
              page &&
              publish.mutate(
                { id: page.id, slug: proposed, indexable },
                { onSuccess: () => onClose() },
              )
            }
          >
            {publish.isPending ? "Publishing…" : "Publish"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-ink-muted">
          Anyone with the address can read this page. It shows what the page
          shows — the same articles, the same arrangement — without any record
          of what you have read.
        </p>

        <Field
          label="Address"
          value={slug}
          onChange={(event) => setSlug(event.target.value)}
          placeholder="comics"
          maxLength={40}
          autoFocus
          hint={<span className="font-mono">{at}</span>}
        />

        {/* Only where the instance allows it. Not disabled — absent: an administrator has
            said no, and a control that shows the choice while refusing it is advertising
            something that is not on offer. */}
        {account.public_indexing ? (
          <label className="flex cursor-pointer items-baseline gap-2 text-sm">
            <input
              type="checkbox"
              checked={indexable}
              onChange={(event) => setIndexable(event.target.checked)}
              className="accent-accent"
            />
            <span>
              <span className="text-ink">Let search engines find it</span>{" "}
              <span className="text-xs text-ink-faint">
                Off by default. This one is hard to undo — a page that has been
                crawled stays in somebody else's cache after it comes down.
              </span>
            </span>
          </label>
        ) : null}

        {publish.error ? <Alert>{publish.error.message}</Alert> : null}
      </div>
    </Modal>
  );
}
