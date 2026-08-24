package migrations

// An edition belongs to a page, and still says whose it is.
//
// Both columns, which looks like one too many until you try to do without either.
//
// page_id is the real owner: a person has several pages now, each composed on its own clock,
// and "the current edition" is a question about a page. principal_id cannot go, because these
// two databases are never ATTACHed and so nothing here can join to pages to find out whose page
// this is. Marking an article read has to reach every one of a person's live editions at once —
// read is a fact about the person and the article, not about which tab they were looking at —
// and without principal_id that query would have to cross the gap between the databases on
// every click.
//
// So it is denormalised on purpose, and the thing that keeps it honest is that both are written
// together by one function. Neither is derived from the other at read time.
var derivedEditionPages = Migration{
	Name: "20260824030100_derived_edition_pages",
	Up: exec(`
ALTER TABLE editions ADD COLUMN page_id TEXT NOT NULL DEFAULT '';

-- The main page's id is the principal's with a page prefix, which is the rule main.db used when
-- it created those rows. Two databases that cannot see each other agreeing on a name is the
-- only way to carry an existing page across without throwing it away and making everybody wait
-- for the next compose.
UPDATE editions SET page_id = 'pg_' || principal_id;

-- A page's editions, newest first, which is how the live one is found.
--
-- Not unique, and that is a change from what came before: there used to be exactly one edition
-- per person, enforced here and maintained by deleting the old one inside the transaction that
-- wrote the new one. Uniqueness bought a constraint nobody was reading and cost a delete on the
-- hot path — and a delete that has to succeed before an insert can is a delete that can fail and
-- take a compose with it. Editions pile up instead; the newest is the page, and the sweep
-- collects the rest.
CREATE INDEX editions_page ON editions(page_id, generated_at DESC);

-- The old unique index said one edition per person, which is precisely what has stopped being
-- true: a person now has one per page. Replaced rather than kept alongside — a unique index on
-- principal_id would refuse the second page's edition outright.
DROP INDEX editions_principal;

-- Everything a person has live at once, for marking an article read across all of them.
CREATE INDEX editions_principal ON editions(principal_id);
`),
}
