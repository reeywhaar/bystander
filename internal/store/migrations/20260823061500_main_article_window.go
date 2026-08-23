package migrations

var mainArticleWindow = Migration{
	Name: "20260823061500_main_article_window",
	Up: exec(`
-- How recent an article has to be to reach somebody's page.
--
-- Seconds, or 0 for no limit. A week by default: a front page is about what is going on,
-- and a fortnight-old article on it is a different kind of object.
--
-- Added rather than folded into the original CREATE TABLE, because the original has
-- shipped. See migrate_test.go.
ALTER TABLE settings ADD COLUMN max_article_age INTEGER NOT NULL DEFAULT 604800
  CHECK (max_article_age IN (0, 86400, 604800, 1209600, 2592000, 31536000));
`),
}
