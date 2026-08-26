package migrations

// An invitation outlives the account it produced.
//
// `principal_id` was `ON DELETE CASCADE`, so deleting an account deleted the invitation that
// made it — and with it the answer to "who let this person in", which is the one question the
// row exists to answer. entities.md has claimed that row is an audit trail since it was
// written; this makes that true.
//
// The column beside it settled the same question the other way already: `created_by` is
// `ON DELETE SET NULL` precisely so that who *issued* an invitation survives the issuer. Who
// it *became* is a fact about the invitation in exactly the same sense, so the asymmetry was
// an oversight rather than a decision.
//
// Nothing about single use depends on this. An accepted invitation is refused by its
// `accepted_at` stamp, which the row keeps either way — before this, a deleted account left no
// row at all and the link died by being unknown. It is the record that was being lost, not the
// guarantee.
//
// SQLite cannot alter a foreign key, so the table is rebuilt: the twelve-step dance from its
// own manual, minus the steps that do not apply — nothing references invites, so no other
// schema mentions it and the rename cannot disturb anything.
//
// Deliberately not `PRAGMA foreign_keys = OFF` first, which the manual's procedure opens with:
// a pragma is a no-op inside a transaction, and every migration here runs in one. It is not
// needed. The rows being copied still point at principals that exist, so nothing is orphaned
// on the way through, and the dropped table is the referring side rather than the referred-to.
var mainInviteSurvivesItsAccount = Migration{
	Name: "20260825235815_main_invite_survives_its_account",
	Up: exec(`
CREATE TABLE invites_kept (
  id           TEXT    PRIMARY KEY,
  token_hash   BLOB    NOT NULL UNIQUE,
  role         TEXT    NOT NULL CHECK (role IN ('admin','user')),
  created_by   TEXT    REFERENCES principals(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  accepted_at  INTEGER,
  -- The change. Nulled rather than cascaded, so the invitation survives to say that an
  -- account was made from it even once that account is gone.
  principal_id TEXT    REFERENCES principals(id) ON DELETE SET NULL,
  email        TEXT    NOT NULL DEFAULT ''
);

INSERT INTO invites_kept
  (id, token_hash, role, created_by, created_at, expires_at, accepted_at, principal_id, email)
SELECT id, token_hash, role, created_by, created_at, expires_at, accepted_at, principal_id, email
  FROM invites;

DROP TABLE invites;
ALTER TABLE invites_kept RENAME TO invites;

CREATE INDEX invites_created_by ON invites(created_by);
`),
}
