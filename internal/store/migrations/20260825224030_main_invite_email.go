package migrations

// An invitation sent to an address rather than handed over.
//
// The address it went to, kept on the invitation, so that accepting it can bind that address to
// the new account as a *proved* recovery address without a second round trip through a code.
//
// That claim is only true if the link went nowhere else, which is why the API does not return
// the link for an invitation it sent. An emailed invitation is delivered by being emailed; one
// an administrator wants to hand over in person is minted without an address and carries none.
// Accepting an emailed one is therefore the same proof a recovery code is — somebody read that
// inbox — obtained from a mail that had to be sent anyway.
//
// Empty rather than NULL for "handed over": every other optional text column here is empty, a
// NULL would make `email <> ”` and `email IS NOT NULL` two spellings of one question, and
// nothing joins on it.
var mainInviteEmail = Migration{
	Name: "20260825224030_main_invite_email",
	Up: exec(`
ALTER TABLE invites ADD COLUMN email TEXT NOT NULL DEFAULT '';
`),
}
