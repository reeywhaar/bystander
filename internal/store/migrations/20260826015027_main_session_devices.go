package migrations

// Where a session was last used from, and with what.
//
// A list of your own sessions is only useful if you can tell them apart well enough to point
// at one and say "not me". A row that says nothing but a time cannot be recognised or
// disowned, so the two things that identify a session in practice are kept beside it: the
// address it was last seen from, and what the browser called itself.
//
// Empty rather than NULL, like every other optional text column here: `<> ”` and
// `IS NOT NULL` should not be two spellings of one question. A session that predates this
// migration has neither, and the interface says so rather than inventing one.
//
// Written on the same throttle as last_seen_at, plus whenever either of them changes — a
// session moving to a different network is the one moment this is worth a write for, and it
// is exactly the moment the throttle would otherwise hide for an hour.
var mainSessionDevices = Migration{
	Name: "20260826015027_main_session_devices",
	Up: exec(`
ALTER TABLE sessions ADD COLUMN last_ip         TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_user_agent TEXT NOT NULL DEFAULT '';
`),
}
