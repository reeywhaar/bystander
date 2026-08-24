package migrations

// A page's filter becomes one list of tags and one list of feeds, each entry taking a side.
//
// It was two modes and two lists: tags read one of three ways, feeds read one of three ways.
// That could say "only Finance" or "everything but Crypto" and never both, which is exactly the
// case tags exist for. Tags overlap — a feed can be Finance and Crypto at once — and the useful
// thing to say about that feed is "Finance, but not the crypto half". Under one mode there was
// no way to say it: excluding Crypto gave up the include, and including Finance took the crypto
// feeds along with it.
//
// So the direction moves off the page and onto each row of its lists. A tag or feed a page has
// no opinion about has no row at all, which is the ordinary case and the reason both lists stay
// short.
//
// # What the two lists mean, and why they are not the same thing
//
// The tags are a funnel. Any include tag present means the page draws only from subscriptions
// carrying one of them; any exclude tag then drops what it matches. Both halves are about sets
// somebody does not enumerate.
//
// The feeds are an override on the result, in both directions: a feed on the include side is on
// the page whatever the tags decided, and a feed on the exclude side is off it whatever they
// decided. That is a stronger thing than the old feed filter was, and a more useful one — the
// two gestures anybody actually makes are "this one as well" and "this one never", and both
// were unsayable when the feed list was a second funnel narrowing the first.
//
// The old 'including' feed mode meant "only these", which has no equivalent here and is not
// missed: with tags doing the narrowing, naming the feeds one by one is what the tag list is
// for. It is translated as the include side regardless, which for such a page would be a
// widening — there were none, on any database this has been run against, and the mode was
// unreachable in the interface beside any tag filter.
//
// # Why three tables are rebuilt to drop two columns
//
// SQLite refuses ALTER TABLE ... DROP COLUMN for a column named in a CHECK constraint, so pages
// has to be rebuilt. Its children hold `ON DELETE CASCADE` back to it, and dropping a parent
// with foreign keys on performs an implicit delete — which would cascade and take every page's
// filter lists with it. Hence the order below: build all three new tables first, so the children
// point at the new parent before the old one is dropped and there is nothing left to cascade
// into. Renaming the parent last rewrites the children's references to it, which is what
// SQLite's non-legacy ALTER TABLE does and the only reason this is short.
var mainPageFilterLists = Migration{
	Name: "20260825020000_main_page_filter_lists",
	Up: exec(`
CREATE TABLE pages_next (
    id               TEXT    NOT NULL PRIMARY KEY,
    principal_id     TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    name             TEXT    NOT NULL,
    slug             TEXT    NOT NULL,
    is_main          INTEGER NOT NULL DEFAULT 0 CHECK (is_main IN (0, 1)),

    edition_interval INTEGER NOT NULL DEFAULT 86400
                       CHECK (edition_interval IN (3600, 21600, 86400, 604800)),
    edition_size     INTEGER NOT NULL DEFAULT 60
                       CHECK (edition_size BETWEEN 10 AND 200),
    next_edition_at  INTEGER NOT NULL,

    max_article_age  INTEGER NOT NULL DEFAULT 0
                       CHECK (max_article_age IN (0, 86400, 604800, 1209600, 2592000, 31536000)),

    created_at       INTEGER NOT NULL,

    UNIQUE (principal_id, slug)
) STRICT;

INSERT INTO pages_next (id, principal_id, name, slug, is_main, edition_interval, edition_size,
                        next_edition_at, max_article_age, created_at)
SELECT id, principal_id, name, slug, is_main, edition_interval, edition_size,
       next_edition_at, max_article_age, created_at
  FROM pages;

CREATE TABLE page_tags_next (
    page_id TEXT NOT NULL REFERENCES pages_next(id) ON DELETE CASCADE,
    tag_id  TEXT NOT NULL REFERENCES tags(id)       ON DELETE CASCADE,
    -- Which side this tag is on. No row means the page has no opinion about it.
    mode    TEXT NOT NULL CHECK (mode IN ('include', 'exclude')),
    -- One row per tag per page, so a tag cannot be on both sides. Drawing from a tag and
    -- dropping it is not a filter with an unlucky answer, it is a contradiction, and a key that
    -- cannot hold one is better than a rule every caller has to remember.
    PRIMARY KEY (page_id, tag_id)
) WITHOUT ROWID;

-- What each page meant under the old mode, said in the new shape. A page filtering on 'no' was
-- not consulting its list at all, so those rows carry no intention worth keeping.
INSERT INTO page_tags_next (page_id, tag_id, mode)
SELECT t.page_id, t.tag_id,
       CASE p.tag_filter WHEN 'excluding' THEN 'exclude' ELSE 'include' END
  FROM page_tags t
  JOIN pages p ON p.id = t.page_id
 WHERE p.tag_filter <> 'no';

CREATE TABLE page_feeds_next (
    page_id TEXT NOT NULL REFERENCES pages_next(id) ON DELETE CASCADE,
    feed_id TEXT NOT NULL REFERENCES feeds(id)      ON DELETE CASCADE,
    -- Which side, and here it overrides the tags rather than narrowing them further: include
    -- puts the feed on the page whatever the tags said, exclude keeps it off whatever they
    -- said. No row means the feed takes whatever the tags decided.
    mode    TEXT NOT NULL CHECK (mode IN ('include', 'exclude')),
    PRIMARY KEY (page_id, feed_id)
) WITHOUT ROWID;

INSERT INTO page_feeds_next (page_id, feed_id, mode)
SELECT f.page_id, f.feed_id,
       CASE p.feed_filter WHEN 'excluding' THEN 'exclude' ELSE 'include' END
  FROM page_feeds f
  JOIN pages p ON p.id = f.page_id
 WHERE p.feed_filter <> 'all';

-- The children go first, so that dropping pages has nothing left pointing at it to cascade
-- into. The new ones already reference pages_next and are untouched by this.
DROP TABLE page_tags;
DROP TABLE page_feeds;
DROP TABLE pages;

-- Renaming the parent rewrites both children's references from pages_next to pages.
ALTER TABLE pages_next RENAME TO pages;
ALTER TABLE page_tags_next RENAME TO page_tags;
ALTER TABLE page_feeds_next RENAME TO page_feeds;

CREATE UNIQUE INDEX pages_main ON pages(principal_id) WHERE is_main = 1;
CREATE INDEX pages_principal ON pages(principal_id, created_at);
CREATE INDEX pages_due ON pages(next_edition_at);
`),
}
