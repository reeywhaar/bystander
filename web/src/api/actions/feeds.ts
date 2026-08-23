import { createApiAction, type ApiAction } from "@app/api/request";
import type { Subscription } from "@app/api/types";

const seg = (value: string) => encodeURIComponent(value);

/** `GET /api/feeds` — this person's subscriptions. */
export function getFeeds(): ApiAction<Subscription[]> {
  return createApiAction((d) => d.call({ method: "GET", path: "/api/feeds" }));
}

/**
 * `POST /api/feeds`
 *
 * The server fetches the URL before accepting it, and follows a web page to the feed it
 * names — so a bare hostname is a fine thing to paste.
 */
export function postFeeds(input: {
  url: string;
  priority?: number;
  tag_ids?: string[];
}): ApiAction<Subscription> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/feeds", body: input }),
  );
}

/** `PATCH /api/feeds/{id}` */
export function patchFeedsById(
  id: string,
  changes: {
    priority?: number;
    title_override?: string;
    tag_ids?: string[];
    article_window?: number;
  },
): ApiAction<Subscription> {
  return createApiAction((d) =>
    d.call({ method: "PATCH", path: `/api/feeds/${seg(id)}`, body: changes }),
  );
}

/** `DELETE /api/feeds/{id}` */
export function deleteFeedsById(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/feeds/${seg(id)}` }),
  );
}
