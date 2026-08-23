package migrations

// A width between half a page and all of it.
//
// The page had three widths — four columns, two, one — on a four-track grid, and all three
// divide four exactly. Every row tiled perfectly, so `grid-auto-flow: dense` never had a hole
// to backfill and never moved a single card: forty-six of fifty-one cards were the same
// quarter-width column, which is a spreadsheet with headlines on it.
//
// The grid is sixteen tracks now, and the widths are 16, 12, 8 and 4 of them — a whole page,
// three quarters, a half, a quarter. Twelve is the new one and it is the one that matters: it
// does not divide sixteen evenly against the others, so a row holding one cannot be filled by
// widths of its own kind and something narrower has to be pulled up beside it.
//
// Every width is a multiple of the narrowest, which is the invariant worth keeping. It means
// whatever a row has left over is also a multiple of the narrowest, so there is always a card
// that fits it — a page can be irregular without ever stranding a gap that nothing can fill,
// and a gap nothing fills reads as a bug rather than as a margin.
//
// This is not decoration. The page is fixed until the next one is composed, and its whole
// purpose is that somebody can come back and find an article they half-remember. Fifty
// identical cards give them one landmark — "somewhere in the middle" — and nothing else. A
// story that is visibly wider than the ones around it is a story that can be looked for.
//
// SQLite cannot alter a CHECK, so the table is rebuilt around the new one. Everything moves
// with it: this is somebody's live front page, and rebuilding it under them would be the one
// thing this reader promises not to do.
var derivedWideSlot = Migration{
	Name: "20260823232919_derived_wide_slot",
	Up: exec(`
CREATE TABLE edition_items_new (
  edition_id TEXT    NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
  item_id    TEXT    NOT NULL REFERENCES items(id)    ON DELETE CASCADE,
  rank       INTEGER NOT NULL,
  slot       TEXT    NOT NULL CHECK (slot IN ('lead','wide','feature','standard','brief')),
  read_at    INTEGER,
  PRIMARY KEY (edition_id, item_id)
) WITHOUT ROWID;

INSERT INTO edition_items_new (edition_id, item_id, rank, slot, read_at)
SELECT edition_id, item_id, rank, slot, read_at FROM edition_items;

DROP TABLE edition_items;
ALTER TABLE edition_items_new RENAME TO edition_items;

CREATE UNIQUE INDEX edition_items_rank ON edition_items(edition_id, rank);
CREATE INDEX        edition_items_item ON edition_items(item_id);
`),
}
