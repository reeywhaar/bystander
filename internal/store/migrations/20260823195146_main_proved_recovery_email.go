package migrations

// An address somebody has proved they can read.
//
// This replaces the plain column added earlier the same day, which stored whatever was
// typed. An address nobody has proved control of is worse than none: a typo sends recovery
// to a stranger's inbox, and the owner finds out at the one moment they cannot afford to —
// when they are already locked out. Worse, a borrowed session could point recovery at an
// address of its own and come back for the account later.
//
// Two tables rather than one row with a "confirmed" flag, because the difference matters:
// an unproved address must never be usable for recovery, and a nullable column is one
// forgotten WHERE clause away from being treated as though it had been. Confirming moves a
// row from one table to the other, so the only address recovery can read is a proved one.
//
// Nothing is carried over from the old column. Those addresses were never proved, and
// migrating them into the proved table would be the entire point of this migration, undone.
var mainProvedRecoveryEmail = Migration{
	Name: "20260823195146_main_proved_recovery_email",
	Up: exec(`
CREATE TABLE user_recovery (
    -- One per account. A second address is a second thing to lose control of, and the case
    -- for it is thin enough to wait for somebody to ask.
    principal_id TEXT    NOT NULL PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
    email        TEXT    NOT NULL,
    confirmed_at INTEGER NOT NULL
) STRICT;

-- One account per address. Two accounts sharing one is two accounts a single inbox can
-- reset, which turns one compromised mailbox into two lost accounts — and makes "who does
-- this address get somebody into" a question with more than one answer.
--
-- NOCASE because nobody thinks of Alice@ and alice@ as two addresses, and every relay that
-- matters agrees with them. It folds ASCII only, which is what addresses are made of in
-- the cases this is protecting against.
CREATE UNIQUE INDEX user_recovery_email ON user_recovery (email COLLATE NOCASE);

-- An address partway through being proved. Replaced rather than accumulated: starting again
-- is what somebody does when the mail did not arrive, and two live codes for one account is
-- two chances at the same guess.
CREATE TABLE recovery_pending (
    principal_id TEXT    NOT NULL PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
    email        TEXT    NOT NULL,
    -- Hashed, like every other secret here. It is short enough to type, which is exactly
    -- why it has no business sitting in the database in the clear.
    code_hash    BLOB    NOT NULL,
    -- Counted, so a code short enough to type cannot be worked through. Bounded attempts
    -- are what make forty bits enough.
    attempts     INTEGER NOT NULL DEFAULT 0,
    expires_at   INTEGER NOT NULL,
    created_at   INTEGER NOT NULL
) STRICT;

ALTER TABLE principals DROP COLUMN recovery_email;
`),
}
