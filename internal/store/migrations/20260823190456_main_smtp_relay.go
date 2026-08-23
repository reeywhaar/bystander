package migrations

// The relay this instance sends mail through.
//
// Configured in the admin interface rather than in the environment, so that setting it up is
// something an operator can do — and correct — from the same place they do everything else,
// without a redeploy to fix a typo in a hostname.
//
// The password is stored as written. There is no vault here to seal it under, and pretending
// otherwise with a reversible scramble would only make it look protected: whoever can read
// main.db can already read the session table and every password hash in it. The file is the
// boundary, and it is the same boundary either way.
//
// One row, enforced rather than assumed. singleton is unique and checked, so a second
// configuration is a constraint violation instead of a quiet question about which one is live.
var mainSMTPRelay = Migration{
	Name: "20260823190456_main_smtp_relay",
	Up: exec(`
CREATE TABLE smtp_config (
    id           TEXT    NOT NULL PRIMARY KEY,
    singleton    INTEGER NOT NULL UNIQUE DEFAULT 1 CHECK (singleton = 1),
    host         TEXT    NOT NULL,
    port         INTEGER NOT NULL,
    -- 'starttls' upgrades a plain connection, 'implicit' is TLS from the first byte. There is
    -- no third option: a password crossing the network in the clear is not a configuration
    -- choice somebody should be able to make by accident.
    tls          TEXT    NOT NULL CHECK (tls IN ('starttls', 'implicit')),
    username     TEXT    NOT NULL,
    password     TEXT    NOT NULL,
    -- What recipients see in From. Separate from the username because the two routinely
    -- differ: relays authenticate an account and send as an address.
    from_address TEXT    NOT NULL,
    -- Optional. The interface defaults the display to "bystander" rather than writing it
    -- here, because a default stored in the row is a default nobody can tell from a choice.
    sender_name  TEXT    NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL
) STRICT;
`),
}
