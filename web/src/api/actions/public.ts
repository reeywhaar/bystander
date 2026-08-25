import { createApiAction, type ApiAction } from "@app/api/request";
import type { PublicPage } from "@app/api/types";

/**
 * `GET /api/public/{person}/{page}` — somebody's published page, to anybody at all.
 *
 * The one call in this application that needs no session. Every way it can fail is a 404: no
 * such person, no such page, taken down, an instance that publishes nothing. A stranger has no
 * business learning which, and an owner already knows.
 */
export function getPublicPage(
  person: string,
  page: string,
): ApiAction<PublicPage> {
  return createApiAction((d) =>
    d.call({
      method: "GET",
      path: `/api/public/${encodeURIComponent(person)}/${encodeURIComponent(page)}`,
    }),
  );
}
