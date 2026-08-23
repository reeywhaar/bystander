import { createApiAction, type ApiAction } from "@app/api/request";
import type { ImportPlan, ImportResult, ImportSelection, Export } from "@app/api/types";

/**
 * `POST /api/feeds/export` — a subscription list, as text.
 *
 * An empty `ids` means everything, which is what "select all" sends rather than
 * enumerating a hundred of them.
 */
export function postFeedsExport(ids: string[]): ApiAction<Export> {
  return createApiAction((d) => d.call({ method: "POST", path: "/api/feeds/export", body: { ids } }));
}

/** `POST /api/feeds/import/preview` — what a pasted list would do, without doing it. */
export function postFeedsImportPreview(opml: string): ApiAction<ImportPlan> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/feeds/import/preview", body: { opml } }),
  );
}

/** `POST /api/feeds/import` — subscribe to exactly what is sent, and nothing else. */
export function postFeedsImport(feeds: ImportSelection[]): ApiAction<ImportResult> {
  return createApiAction((d) => d.call({ method: "POST", path: "/api/feeds/import", body: { feeds } }));
}
