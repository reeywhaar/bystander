package migrations

// How far back a page reaches becomes a property of the feed rather than of the person.
//
// It was one setting for everything somebody read, which is the wrong shape: a news feed
// worth a day and a blog worth a year are exactly the pair a single number cannot serve.
//
// The value each person had is carried onto every feed they follow, so nobody's pages
// change the day this lands — they simply gain the ability to differ afterwards.
//
// Both tables are in main.db, so the move and the drop are one transaction: there is no
// moment where the setting exists in neither place.
var mainArticleWindowPerFeed = Migration{
	Name: "20260823103614_main_article_window_per_feed",
	Up: exec(`
ALTER TABLE subscriptions ADD COLUMN max_article_age INTEGER NOT NULL DEFAULT 604800
  CHECK (max_article_age IN (0, 86400, 604800, 1209600, 2592000, 31536000));

UPDATE subscriptions SET max_article_age = (
  SELECT s.max_article_age FROM settings s WHERE s.principal_id = subscriptions.principal_id
)
WHERE EXISTS (SELECT 1 FROM settings s WHERE s.principal_id = subscriptions.principal_id);

-- Rebuilt rather than ALTER TABLE ... DROP COLUMN, which SQLite refuses for a column named
-- in a CHECK constraint. Nothing references settings, so dropping and renaming is safe with
-- foreign keys on.
CREATE TABLE settings_without_window (
  principal_id     TEXT    PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
  edition_interval INTEGER NOT NULL DEFAULT 86400
                     CHECK (edition_interval IN (3600, 21600, 86400, 604800)),
  edition_size     INTEGER NOT NULL DEFAULT 60
                     CHECK (edition_size BETWEEN 10 AND 200),
  next_edition_at  INTEGER NOT NULL
);

INSERT INTO settings_without_window (principal_id, edition_interval, edition_size, next_edition_at)
  SELECT principal_id, edition_interval, edition_size, next_edition_at FROM settings;

DROP TABLE settings;
ALTER TABLE settings_without_window RENAME TO settings;
CREATE INDEX settings_due ON settings(next_edition_at);
`),
}
