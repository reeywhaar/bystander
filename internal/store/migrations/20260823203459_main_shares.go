package migrations

// A list of feeds, handed to somebody else.
//
// Sharing was a file: export OPML, save it, send it, have the other person find it and
// paste it back. That works on a desk and falls apart between two phones — which is where
// people actually do this, standing next to each other. A link solves it, and a link is
// something to store.
//
// In main.db rather than derived.db, even though it expires in a week. derived.db is the
// disposable half, rebuildable from the feeds themselves; a share is not rebuildable from
// anything, and a link somebody sent to a friend going dead because a cache was rebuilt is
// a promise broken by an implementation detail.
//
// What is stored is the OPML the export already produces — a snapshot, not a reference to
// live subscriptions. What was shared should keep meaning what it meant when it was shared;
// unfollowing something afterwards is not a reason for somebody else's link to change under
// them. It also means the recipient's screen is fed by exactly the code that reads an
// imported file, rather than by a second path that would drift from it.
var mainShares = Migration{
	Name: "20260823203459_main_shares",
	Up: exec(`
CREATE TABLE shares (
    -- The hash of the token in the URL, never the token. The same stance as sessions and
    -- invitations: a link that is lost is made again rather than recovered, and a database
    -- file holds nothing that can be replayed against the instance it came from.
    token_hash   BLOB    NOT NULL PRIMARY KEY,
    principal_id TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    opml         TEXT    NOT NULL,
    -- Kept beside the document so a listing can say how big a share is without parsing it.
    feed_count   INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

-- Expiry is the only thing ever scanned across shares: the sweep that removes the dead ones.
CREATE INDEX shares_expiry ON shares(expires_at);
`),
}
