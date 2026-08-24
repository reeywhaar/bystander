package migrations

// What somebody has read is kept until they stop following the feed.
//
// It was kept for a month and then deleted, which was right while the record's only job was a
// list somebody might look back at. It has a second job now and that job has no expiry: an
// article this person has already read is not offered to any of their pages again, so the
// record is what stops a story coming back a year later as though it were new. A month-long
// memory forgets, and forgetting is the one thing it must not do.
//
// So the two indexes change places. The one on read_at existed for the sweep that deleted by
// age, and there is no such sweep now. What replaces it is a delete by feed, for the moment
// somebody unfollows one — the whole of what they read there goes with it, because the reason
// to keep it went with it.
var derivedReadArticlesKept = Migration{
	Name: "20260824060000_derived_read_articles_kept",
	Up: exec(`
DROP INDEX read_articles_age;

CREATE INDEX read_articles_feed ON read_articles(principal_id, feed_id);
`),
}
