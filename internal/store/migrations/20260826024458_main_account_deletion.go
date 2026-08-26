package migrations

// Deleting your own account, with a week to change your mind.
//
// Two columns rather than one. `deleted_at` is when somebody asked; the account keeps
// working and is erased once the grace period has passed with nobody signing in. Signing in
// is what calls it off — which is also what makes this safe against a borrowed session,
// since whoever really owns the account can undo it by doing the ordinary thing.
//
// `deletion_cancelled_at` is the record that it was called off. Without it the cancellation
// is invisible: an account quietly stops being scheduled for erasure and nothing anywhere
// says why or when. Somebody who asked to be deleted, forgot, and signed in a fortnight
// later is owed the news that their request was withdrawn on their behalf.
//
// Both nullable, like `disabled_at` beside them, because "never" is genuinely absent here
// rather than a zero anybody should have to recognise.
var mainAccountDeletion = Migration{
	Name: "20260826024458_main_account_deletion",
	Up: exec(`
ALTER TABLE principals ADD COLUMN deleted_at            INTEGER;
ALTER TABLE principals ADD COLUMN deletion_cancelled_at INTEGER;
CREATE INDEX principals_deleted ON principals(deleted_at) WHERE deleted_at IS NOT NULL;
`),
}
