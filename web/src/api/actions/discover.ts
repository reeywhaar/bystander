import { createApiAction, type ApiAction } from "@app/api/request";
import type { Candidate } from "@app/api/types";

/**
 * `POST /api/feeds/discover` — what a URL turns out to be, without subscribing to anything.
 *
 * A site usually names more than one feed. This is what lets the interface ask which,
 * rather than taking whichever came first in the markup.
 */
export function postFeedsDiscover(
  url: string,
): ApiAction<{ candidates: Candidate[] }> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/feeds/discover", body: { url } }),
  );
}
