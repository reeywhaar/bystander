package migrations

// What was last sent to a backup store, so an unchanged database is not sent again.
//
// In derived.db, and that is the whole of why this table is here rather than beside the
// settings in main.db. The decision is "has main.db changed since the last push", answered by
// comparing a digest of it — so writing the answer into main.db would change main.db, and
// every check would find a change it had caused itself. Kept in the other database, the
// question stays about what somebody typed.
//
// Losing it costs one redundant upload, which is the right way round for a file this program
// already treats as reconstructible.
var derivedBackupState = Migration{
	Name: "20260903013756_derived_backup_state",
	Up: exec(`
CREATE TABLE backup_state (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  digest    BLOB    NOT NULL,
  pushed_at INTEGER NOT NULL
);
`),
}
