package migrations

// What somebody kept to read later, and the one page that shows it. A save is a copy of the
// article rather than a reference, because the article is pruned with its feed; why, and why
// feed_id is not a foreign key, is under `saved` in docs/entities.md.
//
// A flag beside is_main rather than a kind column: there are two kinds of page and a third is
// not on anybody's list. The check keeps the two flags from describing one page.
var mainSaved = Migration{
	Name: "20261004105013_main_saved",
	Up: exec(`
ALTER TABLE pages ADD COLUMN is_saved INTEGER NOT NULL DEFAULT 0
  CHECK (is_saved IN (0, 1) AND NOT (is_saved = 1 AND is_main = 1));
CREATE UNIQUE INDEX pages_saved ON pages(principal_id) WHERE is_saved = 1;

CREATE TABLE saved (
  principal_id TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  item_id      TEXT    NOT NULL,                 -- derived.db items.id; no FK across databases
  feed_id      TEXT    NOT NULL,
  guid         TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  link         TEXT    NOT NULL,
  author       TEXT    NOT NULL DEFAULT '',
  summary      TEXT    NOT NULL DEFAULT '',        -- sanitized HTML, as items holds it
  image_url    TEXT    NOT NULL DEFAULT '',
  image_width  INTEGER NOT NULL DEFAULT 0,
  image_height INTEGER NOT NULL DEFAULT 0,
  published_at INTEGER NOT NULL,
  source_title TEXT    NOT NULL DEFAULT '',
  source_url   TEXT    NOT NULL DEFAULT '',
  saved_at     INTEGER NOT NULL,
  PRIMARY KEY (principal_id, item_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX saved_when ON saved(principal_id, saved_at DESC);
`),
}
