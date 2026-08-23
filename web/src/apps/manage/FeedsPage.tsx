import { useState, type FormEvent } from "react";

import type { Subscription, Tag } from "@app/api/types";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Priority } from "@app/components/ui/Priority";
import { Spinner } from "@app/components/ui/Spinner";
import { since } from "@app/lib/time";
import {
  useAddFeed,
  useFeeds,
  useRemoveFeed,
  useTags,
  useUpdateFeed,
} from "@app/queries/hooks";

export function FeedsPage() {
  const feeds = useFeeds();
  const tags = useTags();
  const add = useAddFeed();

  const [url, setUrl] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate(
      { url },
      {
        onSuccess: () => setUrl(""),
      },
    );
  }

  if (feeds.isPending || tags.isPending) return <Spinner />;
  if (feeds.error) throw feeds.error;
  if (tags.error) throw tags.error;

  return (
    <div className="flex flex-col gap-8">
      <section>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <div className="flex gap-2">
            <input
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              required
              placeholder="example.com, or a feed URL"
              aria-label="Feed or site address"
              className="flex-1 rounded-md border border-rule bg-paper-raised px-3 py-2 text-sm
                placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-accent"
            />
            <Button type="submit" variant="primary" disabled={add.isPending}>
              {add.isPending ? "Looking…" : "Add"}
            </Button>
          </div>
          <p className="text-xs text-ink-muted">
            A site's address is enough — bystander follows it to the feed it
            names.
          </p>
          {add.error ? <Alert>{add.error.message}</Alert> : null}
        </form>
      </section>

      <section className="flex flex-col gap-1">
        {feeds.data.length === 0 ? (
          <p className="py-10 text-center text-sm text-ink-muted">
            No feeds yet. Add one above, and a page will be composed from it.
          </p>
        ) : (
          feeds.data.map((feed) => (
            <FeedRow key={feed.id} feed={feed} tags={tags.data} />
          ))
        )}
      </section>
    </div>
  );
}

function FeedRow({ feed, tags }: { feed: Subscription; tags: Tag[] }) {
  const update = useUpdateFeed();
  const remove = useRemoveFeed();
  const [open, setOpen] = useState(false);

  const failing = feed.failure_count > 0;

  return (
    <div className="border-b border-rule py-3">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <button
          type="button"
          onClick={() => setOpen((was) => !was)}
          className="text-left font-serif text-lg text-ink hover:text-accent"
          aria-expanded={open}
        >
          {feed.title}
        </button>

        {failing ? (
          <span className="text-xs text-accent" title={feed.last_error}>
            not answering ({feed.failure_count})
          </span>
        ) : feed.last_success_at ? (
          <span className="text-xs text-ink-faint">
            fetched {since(feed.last_success_at)}
          </span>
        ) : (
          <span className="text-xs text-ink-faint">not fetched yet</span>
        )}

        <div className="ml-auto">
          <Priority
            label={`How often ${feed.title} appears`}
            value={feed.priority}
            disabled={update.isPending}
            onChange={(priority) =>
              update.mutate({ id: feed.id, changes: { priority } })
            }
          />
        </div>
      </div>

      {open ? (
        <div className="mt-3 flex flex-col gap-3 pl-1">
          <p className="text-xs break-all text-ink-faint">{feed.url}</p>

          {failing ? <Alert>{feed.last_error}</Alert> : null}

          <div className="flex flex-wrap items-center gap-2">
            {tags.length === 0 ? (
              <p className="text-xs text-ink-muted">
                No tags yet. Tags are how you say which kinds of thing appear
                more often.
              </p>
            ) : (
              tags.map((tag) => {
                const on = feed.tag_ids.includes(tag.id);
                return (
                  <button
                    key={tag.id}
                    type="button"
                    onClick={() =>
                      update.mutate({
                        id: feed.id,
                        changes: {
                          tag_ids: on
                            ? feed.tag_ids.filter((id) => id !== tag.id)
                            : [...feed.tag_ids, tag.id],
                        },
                      })
                    }
                    className={`rounded-full border px-2.5 py-1 text-xs ${
                      on
                        ? "border-accent bg-accent/10 text-accent"
                        : "border-rule text-ink-muted hover:text-ink"
                    }`}
                  >
                    {tag.name}
                  </button>
                );
              })
            )}
          </div>

          <div>
            <Button
              variant="danger"
              onClick={() => remove.mutate(feed.id)}
              disabled={remove.isPending}
            >
              Stop following
            </Button>
          </div>
          {update.error ? <Alert>{update.error.message}</Alert> : null}
          {remove.error ? <Alert>{remove.error.message}</Alert> : null}
        </div>
      ) : null}
    </div>
  );
}
