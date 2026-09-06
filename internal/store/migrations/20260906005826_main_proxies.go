package migrations

// Relays this instance may reach a publisher through when it cannot reach one directly.
//
// In main.db, with the other things somebody typed: a relay is a decision an administrator
// made, it carries a credential, and an instance that lost this table would go on fetching
// and quietly stop being able to reach whatever it was configured to reach.
//
// # Why a kind
//
// Two, and they are not variations on each other. proxio takes `GET /proxy?url=…&token=…` and
// relays what comes back, which is a rewritten URL over the ordinary client. SOCKS5 replaces
// the dialer underneath, so the request is unchanged and the connection is not. The kind is
// what lets one table hold both, and the check constraint keeps the promise honest: a row is
// only ever a kind this program knows how to dial.
//
// # Why a username as well as a token
//
// Because the two kinds do not authenticate the same way. proxio takes one opaque token; SOCKS5
// takes a username and a password. Both go in the same pair of columns — username empty for
// proxio — rather than in a per-kind table, because a relay is one row with one address and one
// credential whichever way it is spoken to.
//
// The credential is never in `url`, which is the column an administrator's browser is shown.
// `socks5://user:pass@host:1080` is the natural way to write one of these down and it would put
// the password on screen and in every log line that printed the address.
//
// # Why a priority rather than a timestamp
//
// They are tried in order, so the order has to be somebody's choice rather than a side effect
// of when each was added. A relay in the same city as the instance and one on another
// continent are not interchangeable, and an administrator who wants the near one first should
// not have to delete and re-add to say so.
//
// 0..100 with the highest tried first, which is the same scale and the same direction as a
// feed's priority. Two numbers in one product that both run 0..100 and disagree about which
// end is which would be a small cruelty. It is not the same *kind* of number — a feed's is a
// probability and this is an ordering — but the direction is what anybody remembers, and 0
// here means tried last rather than never. Never is the enabled column.
var mainProxies = Migration{
	Name: "20260906005826_main_proxies",
	Up: exec(`
CREATE TABLE proxies (
  id         TEXT    NOT NULL PRIMARY KEY,
  kind       TEXT    NOT NULL CHECK (kind IN ('proxio', 'socks5')),
  -- What to call it in a list of them. Optional: the URL is already a name, and a label that
  -- must be filled in is a label full of hostnames typed twice.
  label      TEXT    NOT NULL DEFAULT '',
  -- The relay's own address, without the path it serves on: how that is turned into a request
  -- is the kind's business, not the administrator's. See internal/feeds.
  url        TEXT    NOT NULL,
  -- Empty for a kind that does not use one, which is proxio.
  username   TEXT    NOT NULL DEFAULT '',
  token      TEXT    NOT NULL,
  -- Higher is tried first. Not unique, because reordering a list through a unique index means
  -- shuffling rows around collisions to say something no one is reading concurrently — two
  -- relays at the same priority is a fine thing to mean, and created_at settles them.
  priority   INTEGER NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 100),
  -- Off without being forgotten. A relay that is failing should be switchable off while
  -- somebody works out why, and deleting it loses the token they would need to put it back.
  enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX proxies_order ON proxies(priority DESC, created_at);
`),
}
