import { createApiAction, type ApiAction } from "@app/api/request";
import type { SharedList, ShareLink } from "@app/api/types";

const seg = (value: string) => encodeURIComponent(value);

/**
 * `POST /api/shares` — turns a selection into a link.
 *
 * An empty list means everything, the same as the export means by it. What comes back is
 * the whole URL, not a path: it is about to leave this browser for a message or a camera,
 * and a path is useless in both.
 */
export function postShares(ids: string[]): ApiAction<ShareLink> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/shares", body: { ids } }),
  );
}

/**
 * `GET /api/shares/{token}` — what a link holds.
 *
 * The feeds come back in the same shape a pasted file produces, so the screen that offers
 * them is the one that already exists. Opening a link subscribes nobody to anything.
 */
export function getSharesByToken(token: string): ApiAction<SharedList> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: `/api/shares/${seg(token)}` }),
  );
}
