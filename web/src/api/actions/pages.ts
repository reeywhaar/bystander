import { createApiAction, type ApiAction } from "@app/api/request";
import type { Page } from "@app/api/types";

/** `GET /api/pages` */
export function getPages(): ApiAction<Page[]> {
  return createApiAction((d) => d.call({ method: "GET", path: "/api/pages" }));
}

/** `GET /api/pages/{ref}` — by id or by address. */
export function getPage(ref: string): ApiAction<Page> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: `/api/pages/${encodeURIComponent(ref)}` }),
  );
}

/** `POST /api/pages` */
export function postPage(page: {
  name: string;
  slug: string;
}): ApiAction<Page> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/pages", body: page }),
  );
}

/**
 * What a change to a page can say.
 *
 * Every field optional, and an absent one means "leave it alone" — including the lists, where
 * absent and `[]` are different requests. Sending the mode without the list would leave a page
 * drawing from the wrong things, so the dialog sends both together.
 */
export interface PageChanges {
  name?: string;
  slug?: string;
  edition_interval?: number;
  edition_size?: number;
  max_article_age?: number;
  include_tag_ids?: string[];
  exclude_tag_ids?: string[];
  include_feed_ids?: string[];
  exclude_feed_ids?: string[];
}

/** `PATCH /api/pages/{id}` */
export function patchPage(id: string, changes: PageChanges): ApiAction<Page> {
  return createApiAction((d) =>
    d.call({
      method: "PATCH",
      path: `/api/pages/${encodeURIComponent(id)}`,
      body: changes,
    }),
  );
}

/** `DELETE /api/pages/{id}` */
export function deletePage(id: string): ApiAction<null> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/pages/${encodeURIComponent(id)}` }),
  );
}

/** `PUT /api/pages/{id}/publish` — puts a page on the open web, or moves it. */
export function putPagePublish(
  id: string,
  slug: string,
  indexable: boolean,
): ApiAction<Page> {
  return createApiAction((d) =>
    d.call({
      method: "PUT",
      path: `/api/pages/${encodeURIComponent(id)}/publish`,
      body: { slug, indexable },
    }),
  );
}

/**
 * `DELETE /api/pages/{id}/publish` — takes it down, and remembers where it was.
 *
 * The address is kept so publishing it again offers the one the links already point at.
 * Nobody reaches it in the meantime.
 */
export function deletePagePublish(id: string): ApiAction<Page> {
  return createApiAction((d) =>
    d.call({
      method: "DELETE",
      path: `/api/pages/${encodeURIComponent(id)}/publish`,
    }),
  );
}
