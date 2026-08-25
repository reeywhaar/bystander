package migrations

// A second name, for the pages somebody publishes.
//
// Not the username, and that is the whole reason it exists: a username is a credential half the
// world reuses, and putting a page on the open web should not oblige anybody to announce theirs.
// Two names for two jobs — one to sign in with, one to be known by.
//
// Empty until somebody wants one. A name nobody asked for is a field on a form that has to be
// explained, and most accounts here will never publish anything.
var mainPrincipalSlug = Migration{
	Name: "20260825040000_main_principal_slug",
	Up: exec(`
ALTER TABLE principals ADD COLUMN slug TEXT NOT NULL DEFAULT '';

-- Unique where it is set, and silent where it is not: every account starts without one, and a
-- plain UNIQUE would make the second account the first collision.
CREATE UNIQUE INDEX principals_slug ON principals(slug) WHERE slug <> '';
`),
}
