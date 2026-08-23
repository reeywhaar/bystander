import { createApiAction, type ApiAction } from "@app/api/request";
import type { Edition } from "@app/api/types";

const seg = (value: string) => encodeURIComponent(value);

/** `GET /api/edition` — the live page. Empty `items` before the first is generated. */
export function getEdition(): ApiAction<Edition> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/edition" }),
  );
}

/**
 * `POST /api/edition/regenerate`
 *
 * Refuses with 409 when the feeds have published nothing since the current page was made,
 * and 404 when there is nothing at all yet. Two different situations, two different
 * sentences — see private/docs/api_design.md.
 */
export function postEditionRegenerate(): ApiAction<Edition> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/edition/regenerate" }),
  );
}

/** `PUT /api/edition/items/{id}/read` */
export function putEditionItemsByIdRead(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "PUT", path: `/api/edition/items/${seg(id)}/read` }),
  );
}

/** `DELETE /api/edition/items/{id}/read` */
export function deleteEditionItemsByIdRead(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/edition/items/${seg(id)}/read` }),
  );
}
