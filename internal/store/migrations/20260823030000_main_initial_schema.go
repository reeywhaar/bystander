package migrations

var mainInitialSchema = Migration{
	Name: "20260823030000_main_initial_schema",
	Up: exec(`
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
`),
}
