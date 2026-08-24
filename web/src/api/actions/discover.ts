import { createApiAction, type ApiAction } from "@app/api/request";
import type { FeedPreview, PlannedFeed } from "@app/api/types";

/**
 * `POST /api/feeds/discover` — what a URL turns out to be, without subscribing to anything.
 *
 * A site usually names more than one feed. This is what lets the interface ask which —
 * and it answers in the same shape a pasted list does, so both go through one selection
 * screen rather than two that drift.
 */
export function postFeedsDiscover(
  url: string,
): ApiAction<{ candidates: PlannedFeed[] }> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/feeds/discover", body: { url } }),
  );
}

/**
 * `POST /api/feeds/preview` — what a feed has published, without subscribing to it.
 *
 * A feed's title and address say almost nothing about it: a site offering "Posts",
 * "Comments" and "Notes" is three plausible names and one right answer. This is how somebody
 * finds out before following, rather than by following and then unfollowing again — which
 * leaves read marks and a schedule behind it.
 *
 * Takes a feed's address or a page's; the server resolves a page to the feed it names.
 */
export function postFeedsPreview(url: string): ApiAction<FeedPreview> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/feeds/preview", body: { url } }),
  );
}
