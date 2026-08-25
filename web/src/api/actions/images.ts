import { createApiAction, type ApiAction } from "@app/api/request";
import type { ImageTally } from "@app/api/types";

/** `GET /api/admin/images` — how the pictures on this instance are getting on. */
export function getImages(): ApiAction<ImageTally> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/images" }),
  );
}

/**
 * `POST /api/admin/images/retry` — offer unmeasured pictures back to the queue at once.
 *
 * An empty reason means every picture without a size. Nothing measured is touched.
 */
export function postImagesRetry(reason: string): ApiAction<{ queued: number }> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/admin/images/retry",
      body: { reason },
    }),
  );
}
