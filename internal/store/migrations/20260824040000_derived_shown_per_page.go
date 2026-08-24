package migrations

// What has been shown, remembered per page rather than per person.
//
// It was per person, which was right while a person was a page and wrong the moment they were
// not. A page is a view of what somebody follows, not a share of it: an art story belongs on a
// page of everything and on a page of art, and both should show it. With one record per person
// whichever page composed first took it and the others could never see it again — measured, on
// a real database: a main page composed after an art page held zero art articles, not a few.
//
// So each page keeps its own memory. The cost is rows — the same article is recorded once per
// page that shows it — and a few thousand short rows per page is not a cost worth designing
// around.
//
// What stays shared is whether something has been *read*, which is the other half of the same
// question and has the opposite answer. Reading is a fact about a person and an article, so
// having read it on one page reads it on all of them; that lives in read_articles and in the
// read marks on each edition, and is untouched here.
var derivedShownPerPage = Migration{
	Name: "20260824040000_derived_shown_per_page",
	Up: exec(`
CREATE TABLE shown_per_page (
    page_id   TEXT    NOT NULL,
    feed_id   TEXT    NOT NULL,
    guid_hash BLOB    NOT NULL,
    shown_at  INTEGER NOT NULL,
    PRIMARY KEY (page_id, feed_id, guid_hash)
) WITHOUT ROWID;

-- Everything already shown was shown on the page that was the only page: the main one, whose
-- id is the principal's with a page prefix. The same rule main.db used when it created them.
INSERT INTO shown_per_page (page_id, feed_id, guid_hash, shown_at)
SELECT 'pg_' || principal_id, feed_id, guid_hash, shown_at FROM shown;

DROP TABLE shown;
ALTER TABLE shown_per_page RENAME TO shown;

-- What the retention sweep reads.
CREATE INDEX shown_age ON shown(shown_at);
`),
}
