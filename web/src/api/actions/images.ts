import { createApiAction, type ApiAction } from "@app/api/request";
import type { ImageTally, UnmeasuredImage } from "@app/api/types";

/** `GET /api/admin/images` — how the pictures on this instance are getting on. */
export function getImages(): ApiAction<ImageTally> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/images" }),
  );
}

/**
 * `POST /api/admin/images/retry` — offer unmeasured pictures back to the queue at once.
 *
 * One picture by `url`, or a whole category by `reason` — and neither, which means every
 * picture without a size. Nothing measured is touched by any of them.
 */
export function postImagesRetry(
  what: { reason?: string; url?: string } = {},
): ApiAction<{ queued: number }> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/admin/images/retry",
      body: { reason: what.reason ?? "", url: what.url ?? "" },
    }),
  );
}

/**
 * `GET /api/admin/images/unmeasured` — the pictures behind one of the counts.
 *
 * The reason rides as a query parameter because the empty one is a real group — pictures
 * nothing has asked about yet — and an empty path segment is not a path.
 */
export function getImagesUnmeasured(
  reason: string,
): ApiAction<{ reason: string; limit: number; pictures: UnmeasuredImage[] }> {
  return createApiAction((d) =>
    d.call({
      method: "GET",
      path: "/api/admin/images/unmeasured",
      query: { reason },
    }),
  );
}
