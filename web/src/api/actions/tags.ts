import { createApiAction, type ApiAction } from "@app/api/request";
import type { Tag } from "@app/api/types";

const seg = (value: string) => encodeURIComponent(value);

/** `GET /api/tags` */
export function getTags(): ApiAction<Tag[]> {
  return createApiAction((d) => d.call({ method: "GET", path: "/api/tags" }));
}

/** `POST /api/tags` */
export function postTags(input: {
  name: string;
  parent_id?: string;
  priority?: number;
}): ApiAction<Tag> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/tags", body: input }),
  );
}

/** `PATCH /api/tags/{id}` — refuses a parent that would put a tag inside itself. */
export function patchTagsById(
  id: string,
  changes: { name?: string; parent_id?: string; priority?: number },
): ApiAction<Tag> {
  return createApiAction((d) =>
    d.call({ method: "PATCH", path: `/api/tags/${seg(id)}`, body: changes }),
  );
}

/** `DELETE /api/tags/{id}` — children are promoted to roots, not deleted with it. */
export function deleteTagsById(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/tags/${seg(id)}` }),
  );
}
