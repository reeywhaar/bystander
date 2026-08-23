import { useEffect, useMemo, useState } from "react";

import type { Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { CopyBox } from "@app/components/ui/CopyBox";
import { Modal } from "@app/components/ui/Modal";
import { tagLabel } from "@app/lib/tags";
import { absolute } from "@app/lib/time";
import { useCreateShare, useExportFeeds } from "@app/queries/hooks";

type Shape = "link" | "list" | "opml";

/**
 * Hands somebody else a subscription list.
 *
 * Three shapes, because there are three people on the other end of this. A **link** is for
 * somebody else with an account here: they open it and get the same picker a file would
 * give them, without a file existing at any point. A **list** is for somebody reading a
 * message: names, addresses and the tags they were filed under, which is what makes a
 * stranger's feed legible before they subscribe to it. **OPML** is for their reader, which
 * does not want prose.
 *
 * The link is first because it is the one that works between two phones. Sending a file
 * between them means saving it, finding it, and pasting it back — three steps that each
 * fail differently, and the reason this was worth building.
 */
export function ShareDialog({
  open,
  onClose,
  feeds,
  tags,
}: {
  open: boolean;
  onClose: () => void;
  feeds: Subscription[];
  tags: Tag[];
}) {
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [shape, setShape] = useState<Shape>("link");
  const [copied, setCopied] = useState(false);
  const exportFeeds = useExportFeeds();
  const createShare = useCreateShare();

  // Opening starts from everything, which is what somebody sharing usually means, and
  // leaves unticking as the deliberate act.
  useEffect(() => {
    if (open) setChosen(new Set(feeds.map((feed) => feed.id)));
  }, [open, feeds]);

  const selected = useMemo(
    () => feeds.filter((feed) => chosen.has(feed.id)),
    [feeds, chosen],
  );

  const names = useMemo(() => (id: string) => tagLabel(tags, id), [tags]);

  const asList = useMemo(
    () =>
      selected
        .map((feed) => {
          const labels = feed.tag_ids.map(names).filter(Boolean);
          return (
            feed.title +
            "\n" +
            feed.url +
            (labels.length ? "\n" + labels.join(", ") : "")
          );
        })
        .join("\n\n"),
    [selected, names],
  );

  // The OPML is built by the server, which is the only place that knows how a tag's path
  // is spelled in the file. Asked for whenever the selection changes while that shape is
  // showing.
  useEffect(() => {
    if (!open || shape !== "opml") return;
    exportFeeds.mutate(selected.map((feed) => feed.id));
    // exportFeeds is a mutation object and is not stable across renders; including it
    // would refetch on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, shape, chosen]);

  const text = shape === "list" ? asList : (exportFeeds.data?.opml ?? "");

  // Reset whenever the selection changes, because a link is a snapshot: leaving the old one
  // on screen after unticking something would show a link that no longer says what the
  // ticks say.
  useEffect(() => {
    createShare.reset();
    // createShare is a mutation object and is not stable across renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chosen, open]);

  const link = createShare.data;

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  }

  function download() {
    const blob = new Blob([text], { type: "text/x-opml" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = exportFeeds.data?.filename ?? "feeds.opml";
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Share your feeds"
      footer={
        <>
          {/* Saving a file is a different way of taking the same list away, rather than a
              step towards or away from copying it, so it sits with neither. */}
          {shape === "opml" ? (
            <Button
              className="mr-auto"
              onClick={download}
              disabled={text === ""}
            >
              Save as a file
            </Button>
          ) : null}
          <Button onClick={onClose}>Done</Button>
          {shape === "link" ? (
            <Button
              variant="primary"
              disabled={selected.length === 0 || createShare.isPending}
              onClick={() =>
                createShare.mutate(selected.map((feed) => feed.id))
              }
            >
              {createShare.isPending
                ? "Making…"
                : link
                  ? "Make another"
                  : "Make a link"}
            </Button>
          ) : (
            <Button
              variant="primary"
              onClick={() => void copy()}
              disabled={text === ""}
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-wrap items-center gap-2">
        <Button
          onClick={() => setChosen(new Set(feeds.map((feed) => feed.id)))}
          disabled={selected.length === feeds.length}
        >
          All
        </Button>
        <Button
          onClick={() => setChosen(new Set())}
          disabled={selected.length === 0}
        >
          None
        </Button>
        <span className="ml-auto text-xs text-ink-faint">
          {selected.length} of {feeds.length}
        </span>
      </div>

      <ul className="max-h-56 overflow-y-auto rounded-md border border-rule">
        {feeds.map((feed) => (
          <li key={feed.id} className="border-b border-rule last:border-b-0">
            <label className="flex cursor-pointer items-baseline gap-2 px-3 py-2 text-sm">
              <input
                type="checkbox"
                checked={chosen.has(feed.id)}
                onChange={(event) =>
                  setChosen((was) => {
                    const next = new Set(was);
                    if (event.target.checked) next.add(feed.id);
                    else next.delete(feed.id);
                    return next;
                  })
                }
              />
              <span className="text-ink">{feed.title}</span>
              {feed.tag_ids.length > 0 ? (
                <span className="ml-auto truncate text-xs text-ink-faint">
                  {feed.tag_ids.map(names).filter(Boolean).join(", ")}
                </span>
              ) : null}
            </label>
          </li>
        ))}
      </ul>

      <div className="flex gap-1">
        {(["link", "list", "opml"] as Shape[]).map((option) => (
          <button
            key={option}
            type="button"
            onClick={() => setShape(option)}
            className={`rounded-md border px-2.5 py-1 text-xs ${
              shape === option
                ? "border-accent text-accent"
                : "border-rule text-ink-muted hover:text-ink"
            }`}
          >
            {option === "link"
              ? "As a link"
              : option === "list"
                ? "As a list"
                : "As OPML"}
          </button>
        ))}
        <span className="ml-auto self-center text-xs text-ink-faint">
          {shape === "link"
            ? "for someone here"
            : shape === "list"
              ? "for a message"
              : "for their reader"}
        </span>
      </div>

      {shape === "link" ? (
        <div className="flex flex-col gap-2">
          {createShare.error ? (
            <Alert>{createShare.error.message}</Alert>
          ) : null}
          {link ? (
            <>
              <CopyBox value={link.url} shareTitle="Feeds worth reading" />
              <p className="text-xs text-ink-faint">
                {link.count} feed{link.count === 1 ? "" : "s"}, for anyone with
                an account here. Good until {absolute(link.expires_at)}.
              </p>
            </>
          ) : (
            <p className="text-sm text-ink-muted">
              A link the other person opens on their own account, landing on
              this same list of feeds to pick from. Nothing is sent anywhere,
              and nothing is subscribed to by opening it — the link expires in a
              week.
            </p>
          )}
        </div>
      ) : (
        <textarea
          readOnly
          value={exportFeeds.isPending && shape === "opml" ? "…" : text}
          onFocus={(event) => event.currentTarget.select()}
          rows={8}
          aria-label="Your feeds, ready to share"
          className="w-full resize-y rounded-md border border-rule bg-paper-sunken p-2 font-mono
            text-xs text-ink"
        />
      )}
    </Modal>
  );
}
