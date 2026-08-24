package migrations

// Pages, of which everybody has at least one.
//
// A page was implicit before this: one per person, unnamed, its cadence and size kept in a
// settings table keyed by the person. That table's entire contents were facts about a page —
// how often to compose it, how many articles it holds, when it is next due — so this does not
// add a table beside settings, it renames and generalises the one that was already there.
// Anything genuinely about a person rather than a page would have been the reason to keep
// settings, and there was nothing in it.
//
// The main page is a row like any other, marked with is_main. Not a special case in code and
// not an absent row meaning "the default": a person's pages are a list, and the main one is the
// member of that list that cannot be removed or renamed. Every branch that would otherwise ask
// "is this the implicit page or a real one" is a branch that does not have to exist.
//
// Its slug is empty, because it is served at / rather than at /f/:slug. That keeps
// UNIQUE (principal_id, slug) meaningful — a custom page can never be named into a collision
// with it, since a slug is required to be non-empty for those.
var mainPages = Migration{
	Name: "20260824030000_main_pages",
	Up: exec(`
CREATE TABLE pages (
    id               TEXT    NOT NULL PRIMARY KEY,
    principal_id     TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    -- What it is called in the tabs. The main page's name is fixed by the interface rather
    -- than by a constraint here, because it is displayed and displayed names are translated.
    name             TEXT    NOT NULL,
    -- Empty for the main page, which lives at /. Non-empty and unique per person otherwise.
    slug             TEXT    NOT NULL,
    is_main          INTEGER NOT NULL DEFAULT 0 CHECK (is_main IN (0, 1)),

    -- Cadence and size, moved here from settings unchanged, constraints included. Each page
    -- keeps its own: a page of everything is worth composing daily, and a page filtered to one
    -- tag may only have enough for a week.
    edition_interval INTEGER NOT NULL DEFAULT 86400
                       CHECK (edition_interval IN (3600, 21600, 86400, 604800)),
    edition_size     INTEGER NOT NULL DEFAULT 60
                       CHECK (edition_size BETWEEN 10 AND 200),
    next_edition_at  INTEGER NOT NULL,

    -- How recent an article must be to reach this page. Zero is no limit.
    --
    -- Sits over the top of each feed's own window and the tighter of the two wins. The feed's
    -- window says how long that publisher's articles stay worth reading; this says how current
    -- this particular page is meant to be, which is a different question with a different
    -- answer per page — a finances page wanting today only, out of feeds that are otherwise
    -- worth a week.
    max_article_age  INTEGER NOT NULL DEFAULT 0
                       CHECK (max_article_age IN (0, 86400, 604800, 1209600, 2592000, 31536000)),

    -- How this page chooses what it may draw from. The lists themselves are in page_tags and
    -- page_feeds; these say how to read them.
    --
    -- 'no' and 'all' mean the list is not consulted, and the interface clears it when either is
    -- chosen. Kept as a mode beside a list rather than inferred from an empty list, because
    -- "including nothing" and "not filtering" are different intentions and a reader who empties
    -- a list should not silently get the whole of everything.
    tag_filter       TEXT    NOT NULL DEFAULT 'no'
                       CHECK (tag_filter IN ('no', 'including', 'excluding')),
    feed_filter      TEXT    NOT NULL DEFAULT 'all'
                       CHECK (feed_filter IN ('all', 'including', 'excluding')),

    created_at       INTEGER NOT NULL,

    UNIQUE (principal_id, slug)
) STRICT;

-- One main page each, enforced rather than assumed. The code that creates a person creates
-- their main page in the same transaction, and this is what says so.
CREATE UNIQUE INDEX pages_main ON pages(principal_id) WHERE is_main = 1;

-- Every page a person has, which is the tab strip.
CREATE INDEX pages_principal ON pages(principal_id, created_at);

-- What is due to be composed, which is the scheduler's whole query.
CREATE INDEX pages_due ON pages(next_edition_at);

CREATE TABLE page_tags (
    page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    tag_id  TEXT NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
    PRIMARY KEY (page_id, tag_id)
) WITHOUT ROWID;

CREATE TABLE page_feeds (
    page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    feed_id TEXT NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    PRIMARY KEY (page_id, feed_id)
) WITHOUT ROWID;

-- Everybody who had settings gets a main page holding exactly what their settings held.
--
-- The id is the principal's own with a page prefix. Ids are minted from a prefix and a random
-- tail everywhere else, and this is the one place that cannot: derived.db has to name the same
-- page in its own migration, from the principal id on an edition, without being able to join to
-- this table. A rule both databases can apply is the only thing that works across that gap.
INSERT INTO pages (id, principal_id, name, slug, is_main,
                   edition_interval, edition_size, next_edition_at, created_at)
SELECT 'pg_' || principal_id, principal_id, 'Your page', '', 1,
       edition_interval, edition_size, next_edition_at, unixepoch()
  FROM settings;

-- And anybody who somehow had none gets one on the defaults, so "everybody has a main page" is
-- true of the whole table rather than of the rows that happened to have settings.
INSERT INTO pages (id, principal_id, name, slug, is_main, next_edition_at, created_at)
SELECT 'pg_' || p.id, p.id, 'Your page', '', 1, unixepoch(), unixepoch()
  FROM principals p
 WHERE NOT EXISTS (SELECT 1 FROM pages WHERE principal_id = p.id);

DROP TABLE settings;
`),
}
