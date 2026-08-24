package migrations

// How often a feed is worth fetching, remembered per feed.
//
// It used to be one number for every feed, set by the operator, and it could not have been
// anything else: nobody configuring a reader knows how often each publisher they follow puts
// something out. Measured against nineteen real feeds, one setting meant a comic published
// every three weeks was asked for three hundred and thirty-six times between articles.
//
// So it is computed from what a feed has just published — see feeds.Cadence — and kept here
// because most fetches do not carry any articles to compute it from. A publisher answering 304
// is saying "nothing has changed", which is the commonest answer once a feed has been followed
// for a day, and the interval worked out on the last fetch that did bring articles is the right
// one to keep using.
//
// Zero means it has never been worked out, and the caller reads that as "a day" — the same
// answer a feed with too few articles to judge by gets.
var mainFeedFetchInterval = Migration{
	Name: "20260824080000_main_feed_fetch_interval",
	Up: exec(`
ALTER TABLE feeds ADD COLUMN fetch_interval INTEGER NOT NULL DEFAULT 0;
`),
}
