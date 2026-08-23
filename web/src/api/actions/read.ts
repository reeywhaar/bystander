import { createApiAction, type ApiAction } from "@app/api/request";
import type { ReadArticle } from "@app/api/types";

/** `GET /api/read` — a month of what has been read, newest first. */
export function getRead(): ApiAction<ReadArticle[]> {
  return createApiAction((d) => d.call({ method: "GET", path: "/api/read" }));
}
