import { createApiAction, type ApiAction } from "@app/api/request";
import type { Settings } from "@app/api/types";

/** `GET /api/settings` */
export function getSettings(): ApiAction<Settings> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/settings" }),
  );
}

/** `PATCH /api/settings` */
export function patchSettings(changes: {
  edition_interval?: number;
  edition_size?: number;
  article_window?: number;
}): ApiAction<Settings> {
  return createApiAction((d) =>
    d.call({ method: "PATCH", path: "/api/settings", body: changes }),
  );
}
