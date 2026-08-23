package migrations

// An address an account can be recovered through.
//
// Stored here and nowhere else useful yet: sending anything requires a mail relay, which is
// a separate piece of work. Keeping the column ahead of it means somebody can put their
// address in before it is needed rather than at the moment they cannot get in.
//
// Empty rather than NULL, because "no address" and "the empty address" are the same thing
// and two ways to say it is one too many.
var mainRecoveryEmail = Migration{
	Name: "20260823190043_main_recovery_email",
	Up: exec(`
ALTER TABLE principals ADD COLUMN recovery_email TEXT NOT NULL DEFAULT '';
`),
}
