package migrations

// Which relay last reached a publisher, so the next request there does not have to find out
// again.
//
// A publisher that blocks this instance blocks it every time. Without this, every fetch of
// every blocked feed — and every picture beside it — pays for a direct request that is known to
// fail before the relay is reached for, which on a feed checked hourly is a request an hour
// spent learning something already learned.
//
// # In derived.db
//
// It is not a setting, it is something the machine noticed. Losing it costs one failed direct
// request per domain and then it is known again, which is exactly the bargain this database
// exists to make. It also means no foreign key to proxies, which lives in main.db and cannot be
// referenced from here — so a row naming a relay that has since been deleted is expected rather
// than impossible, and reads have to tolerate it.
//
// # Keyed by registrable domain
//
// Not by host. A publisher serving its feed from www and its pictures from an images subdomain
// is one publisher with one block, and learning the two separately means paying for the lesson
// twice. eTLD+1 is what makes that generalisation safe: it groups foo.example.com with
// bar.example.com and does not group two unrelated sites that happen to share .co.uk.
var derivedProxyRoutes = Migration{
	Name: "20260906011118_derived_proxy_routes",
	Up: exec(`
CREATE TABLE proxy_routes (
  domain     TEXT    NOT NULL PRIMARY KEY,
  proxy_id   TEXT    NOT NULL,
  -- When this was last confirmed. A route is not trusted forever: a publisher that lifts a
  -- block would otherwise be reached through a relay for the rest of the instance's life,
  -- because nothing would ever try the direct route again to find out.
  updated_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;
`),
}
