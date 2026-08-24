package migrations

// What the server actually said, when a feed stops answering.
//
// last_error already held a sentence — "the server answered 503 Service Unavailable" — which
// says a fetch failed and nothing about why. The half that is useful is the body underneath it:
// a rate-limit note, a "this feed has moved", a login page where a feed used to be. It was read
// far enough to be discarded and is now kept.
//
// Bounded, and deliberately small. This is here to be read by a person wondering what happened
// to their feed, and two kilobytes is a JSON error, an HTML title, or the top of a stack trace
// — everything past that is somebody else's error page, stored once per feed for as long as the
// feed keeps failing.
//
// Empty when the request never reached a server at all, which last_status already distinguishes
// by being zero. The two together are the whole answer: nobody answered, or this is what they
// said.
var mainFeedErrorBody = Migration{
	Name: "20260824070000_main_feed_error_body",
	Up: exec(`
ALTER TABLE feeds ADD COLUMN last_error_body TEXT NOT NULL DEFAULT '';
`),
}
