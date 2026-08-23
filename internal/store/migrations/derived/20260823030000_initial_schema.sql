-- feed_id references main.db; there is no FK, because no constraint can cross a database
-- and no transaction may span the two. A row pointing at a deleted feed is garbage to be
-- collected, not an inconsistency to be repaired.
--
-- summary is sanitized on the way in, not on the way out, so every reader of this table
-- gets the safe form by construction and a bug in a renderer cannot become an injection.
CREATE TABLE items (
  id           TEXT    PRIMARY KEY,
  feed_id      TEXT    NOT NULL,
  guid         TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  link         TEXT    NOT NULL,
  author       TEXT    NOT NULL DEFAULT '',
  summary      TEXT    NOT NULL DEFAULT '',
  image_url    TEXT    NOT NULL DEFAULT '',
  published_at INTEGER NOT NULL,
  fetched_at   INTEGER NOT NULL,
  UNIQUE (feed_id, guid)
);
CREATE INDEX items_feed_published ON items(feed_id, published_at DESC);
CREATE INDEX items_fetched        ON items(fetched_at);

-- The unique index on principal_id is the invariant made structural: exactly one live
-- edition per person. "Old ones are gone for good" is enforced by the schema rather than
-- by remembering to delete.
CREATE TABLE editions (
  id           TEXT    PRIMARY KEY,
  principal_id TEXT    NOT NULL,
  generated_at INTEGER NOT NULL,
  seed         INTEGER NOT NULL,
  size         INTEGER NOT NULL
);
CREATE UNIQUE INDEX editions_principal ON editions(principal_id);

-- read_at lives here and not on items. That single choice is why derived.db can be
-- deleted without a conversation: a read mark belongs to the edition it was made in, so
-- when the edition goes the mark goes with it. Nothing to migrate, nothing to reconcile,
-- and nobody inherits a four-year read history they cannot see.
CREATE TABLE edition_items (
  edition_id TEXT    NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
  item_id    TEXT    NOT NULL REFERENCES items(id)    ON DELETE CASCADE,
  rank       INTEGER NOT NULL,
  slot       TEXT    NOT NULL CHECK (slot IN ('lead','feature','standard','brief')),
  read_at    INTEGER,
  PRIMARY KEY (edition_id, item_id)
) WITHOUT ROWID;
CREATE UNIQUE INDEX edition_items_rank ON edition_items(edition_id, rank);
CREATE INDEX        edition_items_item ON edition_items(item_id);

-- The one thing that outlives an edition, and deliberately tiny: a truncated hash, not a
-- row of content. Without it the same article resurfaces every cycle and the front page
-- stops feeling like one. Pruned at three times the item retention, so a hash always
-- outlives the item it refers to.
CREATE TABLE shown (
  principal_id TEXT    NOT NULL,
  feed_id      TEXT    NOT NULL,
  guid_hash    BLOB    NOT NULL,
  shown_at     INTEGER NOT NULL,
  PRIMARY KEY (principal_id, feed_id, guid_hash)
) WITHOUT ROWID;
CREATE INDEX shown_age ON shown(shown_at);
