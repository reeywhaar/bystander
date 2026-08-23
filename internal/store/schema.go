package store

// Migrations are append-only and tracked by PRAGMA user_version. Each entry is applied in
// its own transaction that also stamps the version.
//
// NEVER EDIT A RELEASED ENTRY. Every deployment past that point has already recorded it as
// applied and will skip the edit, so the schema in front of the code silently stops
// matching the schema in the file. Add a new entry instead.
//
// The full argument for each table — and for which database it lives in — is in
// private/docs/entities.md.

// mainMigrations owns what a person typed. This is the database worth backing up.
var mainMigrations = []string{
	`
-- Accounts. COLLATE NOCASE on the username because "Alice" and "alice" are the same
-- person trying to log in, and letting both exist is a support ticket waiting to happen.
CREATE TABLE principals (
  id            TEXT    PRIMARY KEY,
  username      TEXT    NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT    NOT NULL,
  role          TEXT    NOT NULL CHECK (role IN ('admin','user')),
  created_at    INTEGER NOT NULL,
  disabled_at   INTEGER
);

-- Keyed by sha256 of the cookie value, never the value. A backup, a heap dump or a
-- swapped page therefore never contains something replayable as a credential, and the
-- lookup is timing-safe without trying to be: telling two rows apart by timing would
-- require finding a 256-bit preimage first.
CREATE TABLE sessions (
  id_hash      BLOB    PRIMARY KEY,
  principal_id TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);
CREATE INDEX sessions_expires   ON sessions(expires_at);
CREATE INDEX sessions_principal ON sessions(principal_id);

-- Single use, hashed for the same reason sessions are. An accepted invitation keeps its
-- row and points at the principal it produced: that is the record of where an account
-- came from, which is why accepting stamps accepted_at rather than deleting.
CREATE TABLE invites (
  id           TEXT    PRIMARY KEY,
  token_hash   BLOB    NOT NULL UNIQUE,
  role         TEXT    NOT NULL CHECK (role IN ('admin','user')),
  created_by   TEXT    REFERENCES principals(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  accepted_at  INTEGER,
  principal_id TEXT    REFERENCES principals(id) ON DELETE CASCADE
);
CREATE INDEX invites_created_by ON invites(created_by);

-- Per principal: a taxonomy is a personal thing, and a shared one would need an owner and
-- a merge policy nobody asked for.
--
-- ON DELETE SET NULL promotes children to roots rather than cascading. Deleting "News"
-- should not silently delete everything filed under it.
CREATE TABLE tags (
  id           TEXT    PRIMARY KEY,
  principal_id TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  name         TEXT    NOT NULL,
  parent_id    TEXT    REFERENCES tags(id) ON DELETE SET NULL,
  priority     INTEGER NOT NULL DEFAULT 50 CHECK (priority BETWEEN 0 AND 100),
  created_at   INTEGER NOT NULL
);
-- Over ifnull(parent_id,'') rather than parent_id, because SQLite treats NULLs as
-- DISTINCT in a UNIQUE constraint: without the ifnull, two root tags could both be called
-- "Art" and neither the database nor the interface would object.
CREATE UNIQUE INDEX tags_name   ON tags(principal_id, ifnull(parent_id,''), name);
CREATE INDEX        tags_parent ON tags(parent_id);

-- Feeds are global, not per user. Two people following the same URL cause one fetch,
-- which matters to the publisher more than to us, and makes the poller's work
-- proportional to distinct URLs rather than to subscriptions.
--
-- canonical_url is the dedup key; url keeps what was typed so an error can quote it back.
CREATE TABLE feeds (
  id              TEXT    PRIMARY KEY,
  url             TEXT    NOT NULL,
  canonical_url   TEXT    NOT NULL UNIQUE,
  title           TEXT    NOT NULL DEFAULT '',
  site_url        TEXT    NOT NULL DEFAULT '',
  etag            TEXT    NOT NULL DEFAULT '',
  last_modified   TEXT    NOT NULL DEFAULT '',
  last_fetch_at   INTEGER,
  last_success_at INTEGER,
  last_status     INTEGER,
  last_error      TEXT    NOT NULL DEFAULT '',
  failure_count   INTEGER NOT NULL DEFAULT 0,
  next_fetch_at   INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL
);
CREATE INDEX feeds_due ON feeds(next_fetch_at);

-- Everything a person chose about a feed lives here; everything the fetcher learned lives
-- on feeds.
CREATE TABLE subscriptions (
  id             TEXT    PRIMARY KEY,
  principal_id   TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  feed_id        TEXT    NOT NULL REFERENCES feeds(id)      ON DELETE CASCADE,
  title_override TEXT    NOT NULL DEFAULT '',
  priority       INTEGER NOT NULL DEFAULT 50 CHECK (priority BETWEEN 0 AND 100),
  created_at     INTEGER NOT NULL,
  UNIQUE (principal_id, feed_id)
);
CREATE INDEX subscriptions_feed ON subscriptions(feed_id);

CREATE TABLE subscription_tags (
  subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  tag_id          TEXT NOT NULL REFERENCES tags(id)          ON DELETE CASCADE,
  PRIMARY KEY (subscription_id, tag_id)
) WITHOUT ROWID;
CREATE INDEX subscription_tags_tag ON subscription_tags(tag_id);

-- A row is created with the principal, so the scheduler never has to cope with its
-- absence. The interval is a closed set because an arbitrary cron expression is a support
-- burden with no matching demand, and four options fit in a segmented control.
CREATE TABLE settings (
  principal_id     TEXT    PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
  edition_interval INTEGER NOT NULL DEFAULT 86400
                     CHECK (edition_interval IN (3600, 21600, 86400, 604800)),
  edition_size     INTEGER NOT NULL DEFAULT 60
                     CHECK (edition_size BETWEEN 10 AND 200),
  next_edition_at  INTEGER NOT NULL
);
CREATE INDEX settings_due ON settings(next_edition_at);
`,
}

// derivedMigrations owns what the machine produced. Everything here is reconstructible
// from main.db plus one fetch cycle, and nothing here is worth a backup.
var derivedMigrations = []string{
	`
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
`,
	`
-- What somebody actually read, kept for a month.
--
-- A read mark on edition_items belongs to its page and dies with it — that is what lets a
-- page be discarded without a conversation. This is a different thing: a record that
-- survives the page, so "what did I read last week" has an answer even though last week's
-- page is gone.
--
-- It is not an unread count in disguise. It counts nothing, it lists only what has been
-- dealt with, and it expires. The reason it can exist at all is that a list of things you
-- have finished with asks nothing of you.
--
-- Denormalised on purpose. The row has to outlive the item — pruned at 30 days — and the
-- subscription, which somebody may drop the day after reading something. The feed title is
-- not stored because it lives in the other database; feed_id is kept so the interface can
-- name the source while the subscription still exists.
CREATE TABLE read_articles (
  principal_id TEXT    NOT NULL,
  item_id      TEXT    NOT NULL,
  feed_id      TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  link         TEXT    NOT NULL,
  published_at INTEGER NOT NULL,
  read_at      INTEGER NOT NULL,
  PRIMARY KEY (principal_id, item_id)
) WITHOUT ROWID;
CREATE INDEX read_articles_when ON read_articles(principal_id, read_at DESC);
CREATE INDEX read_articles_age  ON read_articles(read_at);
`,
}
