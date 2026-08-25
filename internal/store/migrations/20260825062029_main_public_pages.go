package migrations

// Putting a page on the open web: where it lives, whether it is up, and who decides.
//
// **publish_slug** is separate from pages.slug, and has to be: the main page has no slug at all
// — it is served at / rather than at /f/:slug — and it is the page most people would publish
// first. Kept when a page is taken down rather than cleared, so that publishing it again offers
// the address the links already point at.
//
// **published** is what makes it reachable, and it is a column of its own for exactly that
// reason. One flag, one meaning: taking a page down does not throw away where it was.
//
// **indexable** is the owner's answer to "should a search engine keep this", and the instance's
// answer overrules it. The two are an AND, and where the instance says no the control is not
// shown at all rather than shown and ignored.
//
// # Why the instance gets a say
//
// Everything on instance_settings is off until an administrator says otherwise, and the
// asymmetry between the two switches is the point. Publishing is reversible: take the page
// down and it is down. Indexing is not — a page that has been crawled stays in somebody else's
// cache long after it is gone, and no switch here reaches that. So the instance decides whether
// either is possible at all, and both answers start as no.
var mainPublicPages = Migration{
	Name: "20260825062029_main_public_pages",
	Up: exec(`
ALTER TABLE pages ADD COLUMN publish_slug TEXT NOT NULL DEFAULT '';
ALTER TABLE pages ADD COLUMN published    INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1));
ALTER TABLE pages ADD COLUMN indexable    INTEGER NOT NULL DEFAULT 0 CHECK (indexable IN (0, 1));

-- Per person, because the address is /p/<their name>/<this one>: two people may both publish a
-- page called "comics" and neither should have to find that out from an error.
CREATE UNIQUE INDEX pages_publish_slug ON pages(principal_id, publish_slug) WHERE publish_slug <> '';

-- What is up, which is the public lookup's whole query once it has resolved the person.
CREATE INDEX pages_published ON pages(published) WHERE published = 1;

-- The instance's own answers, of which there are two so far.
--
-- Shaped like smtp_config: a singleton row held down by a CHECK, rather than a key-value table
-- that would make every setting a string and every read a parse. A third setting is a column.
CREATE TABLE instance_settings (
    id              TEXT    NOT NULL PRIMARY KEY,
    singleton       INTEGER NOT NULL UNIQUE DEFAULT 1 CHECK (singleton = 1),
    -- Whether anybody here may publish a page at all. Off, like everything else on this row:
    -- an instance that serves nothing to strangers should not begin serving to strangers
    -- because it was upgraded. Somebody has to decide, and the safe answer is the one that
    -- happens when nobody does.
    --
    -- Turning it off again takes every published page down rather than only stopping new ones:
    -- this is the instance's answer, not a default for pages to inherit.
    public_pages    INTEGER NOT NULL DEFAULT 0 CHECK (public_pages IN (0, 1)),
    -- Whether a published page may ask to be indexed. Also off, and for a stronger reason:
    -- publishing is reversible and indexing is not.
    public_indexing INTEGER NOT NULL DEFAULT 0 CHECK (public_indexing IN (0, 1)),
    updated_at      INTEGER NOT NULL
) STRICT;
`),
}
