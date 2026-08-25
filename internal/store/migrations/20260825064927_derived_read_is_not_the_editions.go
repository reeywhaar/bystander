package migrations

// A read mark stops being a column on the edition.
//
// It was there first, and the note in the initial schema said why: "a read mark belongs to the
// edition it was made in, so when the edition goes the mark goes with it". That was true and
// then stopped being true. read_articles arrived — first as a month's record, later kept until
// somebody unfollows the feed — and composing a page began *copying* the mark forward out of it
// so that an article shown again did not come back looking new.
//
// From that moment the column was a cache of a fact with a different home, and every write had
// to remember to keep it in step. Marking a whole feed read wrote both. Unmarking wrote both.
// Reading one article wrote both, and could only do so for an article on one of your own pages
// — which is why marking something read on a page somebody else published had no way to work at
// all.
//
// So: one home. The edition says what is on the page. Whether somebody has read it is a fact
// about them and the article, joined when the page is read — and joined against *whoever is
// looking*, which is what lets a visitor see their own reading on a page they did not compose.
var derivedReadIsNotTheEditions = Migration{
	Name: "20260825064927_derived_read_is_not_the_editions",
	Up: exec(`
ALTER TABLE edition_items DROP COLUMN read_at;
`),
}
