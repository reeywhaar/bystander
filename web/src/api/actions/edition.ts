import { createApiAction, type ApiAction } from "@app/api/request";
import type { Edition } from "@app/api/types";

const seg = (value: string) => encodeURIComponent(value);

/**
 * `GET /api/edition` — the live page. Empty `items` before the first is generated.
 *
 * `page` names one of this person's pages, by address or by id. Empty is the main page, which
 * is also what the endpoint answers when the parameter is absent — so the reader at `/` asks
 * for nothing in particular and gets the right thing.
 */
export function getEdition(page = ""): ApiAction<Edition> {
  return createApiAction((d) =>
    d.call({
      method: "GET",
      path: "/api/edition",
      ...(page ? { query: { page } } : {}),
    }),
  );
}

/**
 * `POST /api/edition/regenerate`
 *
 * Refuses with 409 when the feeds have published nothing since the current page was made,
 * and 404 when there is nothing at all yet. Two different situations, two different
 * sentences — see private/docs/api_design.md.
 */
export function postEditionRegenerate(page = ""): ApiAction<Edition> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/edition/regenerate",
      ...(page ? { query: { page } } : {}),
    }),
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
