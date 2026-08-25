package migrations

// When a picture may be asked about again, rather than whether it ever was.
//
// image_probed was a flag, and the flag was a trap: it was set on *any* failure, and the query
// that hands out work only offered pictures where it was still zero. So one timeout, one rate
// limit, one bad minute at a CDN, and that picture was never measured again — for as long as
// the article existed.
//
// It was not theoretical. On a real instance, fifteen of the nineteen pictures on a comics page
// were stuck this way; replayed by hand against the same hosts with the same headers, sixteen
// of the twenty stuck pictures measured perfectly on the first try. Nothing was wrong with them
// except that they had been asked once, at a bad moment, and could not be asked again.
//
// A time rather than a flag, and the time each failure earns is its own: a server that said it
// was having trouble is asked again within the hour, and one that gave a settled answer is left
// for a day. See feeds.MeasureRetry.
//
// Everything starts at zero, which means "ask now". That is deliberate: it is what heals the
// pictures already stuck when this lands. Pictures that were measured are never offered again
// regardless, because the query also requires the size to still be missing — the answer is what
// ends the asking, not the asking.
//
// image_error is why it failed, as a category rather than a message: "gone", "refused", "busy",
// "unreachable", "undecodable", "empty". A message is for reading once; a category is something
// a later version can act on. When a decoder is added for a format there was none for, the
// migration that adds it can say
//
//	UPDATE items SET image_retry_at = 0 WHERE image_error = 'undecodable'
//
// and every picture that was unreadable for that reason — and only those — is asked again the
// same day. Without the column that is a choice between re-probing everything and re-probing
// nothing.
var derivedImageRetryAt = Migration{
	Name: "20260825030000_derived_image_retry_at",
	Up: exec(`
ALTER TABLE items ADD COLUMN image_retry_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN image_error TEXT NOT NULL DEFAULT '';
ALTER TABLE items DROP COLUMN image_probed;
`),
}
