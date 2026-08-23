import type { Tag } from "@app/api/types";

/** Guards against a cycle the server should already have refused. */
const MAX_DEPTH = 32;

/**
 * A tag's ancestry, root first: `["News", "World"]` for "World" nested under "News".
 *
 * This is how a tag travels between instances — ids mean nothing outside the one that
 * minted them — so it is what the import and export both speak in.
 */
export function tagPath(tags: Tag[], id: string): string[] {
  const byID = new Map(tags.map((tag) => [tag.id, tag]));
  const path: string[] = [];

  for (
    let at = byID.get(id);
    at;
    at = at.parent_id ? byID.get(at.parent_id) : undefined
  ) {
    path.unshift(at.name);
    if (path.length > MAX_DEPTH) break;
  }
  return path;
}

/** The same thing for reading: "News / World". */
export function tagLabel(tags: Tag[], id: string): string {
  return tagPath(tags, id).join(" / ");
}
