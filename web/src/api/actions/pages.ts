import { createApiAction, type ApiAction } from "@app/api/request";
import type { FeedFilter, Page, TagFilter } from "@app/api/types";

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
  tag_filter?: TagFilter;
  feed_filter?: FeedFilter;
  tag_ids?: string[];
  feed_ids?: string[];
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
