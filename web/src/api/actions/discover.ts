import { createApiAction, type ApiAction } from "@app/api/request";
import type { PlannedFeed } from "@app/api/types";

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
