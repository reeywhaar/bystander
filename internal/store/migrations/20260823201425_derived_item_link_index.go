package migrations

// What an article's link is, made findable.
//
// An article's identity is the publisher's guid, and plenty of publishers cannot keep one
// still. theblueprint.ru appends the publication time to the permalink inside the guid, so
// editing an article changes the one field whose entire job is to stay the same; every edit
// then arrives as a new article and the same story sits on the page twice, the old headline
// beside the new one.
//
// The link is the other thing an article carries that is supposed to identify it, and when
// the guid moves it usually does not. This index is what lets ingest ask "do we already have
// this link?" cheaply enough to ask it about every article of every fetch.
//
// Not a unique index. Feeds exist whose items all point at one page, and a constraint would
// turn those into a feed with one article in it — losing articles to prevent duplicates is
// the wrong way round. The rule is applied in code, where it can be careful.
var derivedItemLinkIndex = Migration{
	Name: "20260823201425_derived_item_link_index",
	Up: exec(`
CREATE INDEX items_feed_link ON items(feed_id, link);
`),
}
