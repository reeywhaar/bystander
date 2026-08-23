package migrations

var derivedReadArticles = Migration{
	Name: "20260823051000_derived_read_articles",
	Up: exec(`
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
`),
}
