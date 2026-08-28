package migrations

// A link that sets a new password without knowing the old one.
//
// Not the recovery *address*, which is user_recovery from 20260823195146_main_proved_recovery_email.
// That is where a link can be sent; this is the link.
//
// The same shape as invites and for the same reasons: the token is a bearer credential, so
// only its hash is kept and a lost link is reissued rather than recovered; and the row
// survives being spent, because "when was this account last recovered, and who asked" is the
// question somebody investigating a takeover has to be able to answer.
//
// created_by is the administrator who issued it, and NULL when nobody did — a link asked for
// through the forgotten-password form has no author but whoever can read the mailbox. SET NULL
// rather than CASCADE, so removing an administrator does not erase the record of what they
// handed out.
//
// Nothing here is unique per principal. Issuing a link neither cancels the last one nor
// touches the password somebody still knows: it is an extra door, not a new lock. Spending
// one does close the others, and voided_at is where that is written down — a link that was
// still outstanding when a different one was used, which from here on cannot be told apart
// from a stolen one.
var mainRecoveryLinks = Migration{
	Name: "20260828221340_main_recovery_links",
	Up: exec(`
CREATE TABLE recovery_links (
  id           TEXT    PRIMARY KEY,
  token_hash   BLOB    NOT NULL UNIQUE,
  principal_id TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  created_by   TEXT    REFERENCES principals(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  used_at      INTEGER,
  voided_at    INTEGER
);
CREATE INDEX recovery_links_principal ON recovery_links(principal_id);
`),
}
