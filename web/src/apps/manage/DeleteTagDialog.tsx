import type { Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Modal } from "@app/components/ui/Modal";
import { useRemoveTag } from "@app/queries/hooks";

/**
 * Deleting a tag, and what goes with it.
 *
 * None of what goes with it is obvious, which is the reason this is asked rather than done: a
 * tag nested under this one is *promoted* rather than deleted, the feeds filed here keep
 * everything but the filing, and any page with a rule about this tag quietly loses that rule.
 * "Delete" leads nobody to expect the first of those.
 */
export function DeleteTagDialog({
  tag,
  tags,
  onClose,
}: {
  /** The tag being deleted, or null when the dialog is shut. */
  tag: Tag | null;
  /** All of them, to count what is nested under this one. */
  tags: Tag[];
  onClose: () => void;
}) {
  const remove = useRemoveTag();

  if (!tag) return null;
  const children = tags.filter((other) => other.parent_id === tag.id).length;

  return (
    <Modal
      open
      onClose={onClose}
      title={`Delete ${tag.name}?`}
      footer={
        <>
          <Button onClick={onClose} disabled={remove.isPending}>
            Keep it
          </Button>
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(tag.id, { onSuccess: onClose })}
          >
            {remove.isPending ? "Deleting…" : "Delete it"}
          </Button>
        </>
      }
    >
      <p className="text-sm text-ink-muted">
        Every feed filed under it is unfiled, and any page that had a rule about
        it loses that rule. The feeds and the pages themselves stay.
      </p>

      {/* The surprising half, and only said when there is something to say. */}
      {children > 0 ? (
        <p className="text-sm text-ink-muted">
          {children === 1
            ? "The tag nested under it is not deleted with it — it becomes a tag of its own."
            : `The ${children} tags nested under it are not deleted with it — they become tags of their own.`}
        </p>
      ) : null}

      {remove.error ? <Alert>{remove.error.message}</Alert> : null}
    </Modal>
  );
}
