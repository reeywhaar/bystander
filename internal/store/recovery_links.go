package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"bystander/internal/ids"
)

// This file is the recovery *link*. recovery.go is the recovery *address* — the inbox an
// account has proved it can read. The two meet in one place: a forgotten-password request
// looks up the address and sends a link to it.

// RecoveryLinkTTL is how long a link stays usable.
//
// A day, against an invitation's week. An invitation creates an account nobody has yet, and
// waits for somebody to get round to it; this one opens an account that already exists and
// is being used, so it should stop working long before anybody has forgotten they asked for
// it. A day still survives being sent in the evening and read the next morning.
const RecoveryLinkTTL = 24 * time.Hour

// recoveryTokenBytes is the entropy in a recovery token — 256 bits, the same standard the
// session id and the invitation token are held to, and for the same reason: whoever holds
// this can take the account.
const recoveryTokenBytes = 32

// RecoveryLink is one issued way back into an account.
//
// It keeps its row after it is spent. That row is the record of how somebody got back in and
// who let them, which is the first thing anybody looking into a stolen account wants.
type RecoveryLink struct {
	ID          string
	PrincipalID string
	CreatedBy   string // empty when the account asked for it themselves, or its issuer is gone
	CreatedAt   time.Time
	ExpiresAt   time.Time
	UsedAt      time.Time // zero while outstanding
	// VoidedAt is when a different link for this account was spent instead. See UseRecoveryLink.
	VoidedAt time.Time
}

// Used reports whether this link has already set a password.
func (l *RecoveryLink) Used() bool { return !l.UsedAt.IsZero() }

// Voided reports whether another link for the same account was spent while this one was out.
func (l *RecoveryLink) Voided() bool { return !l.VoidedAt.IsZero() }

// Expired reports whether this link has lapsed.
func (l *RecoveryLink) Expired(now time.Time) bool { return now.After(l.ExpiresAt) }

// Usable reports whether this link can still set a password.
func (l *RecoveryLink) Usable(now time.Time) bool {
	return !l.Used() && !l.Voided() && !l.Expired(now)
}

// CreateRecoveryLink mints a way back into an account and returns it with its token.
//
// createdBy is the administrator who asked, or empty when the account asked for itself.
//
// The token is returned exactly once, here. What is stored is its hash, so a link that gets
// lost is reissued rather than recovered — the same stance as sessions and invitations, and
// the same reason: nothing in the database should be replayable by whoever can read it.
//
// This changes nothing about the account. No session ends, no password moves, and nobody is
// told: somebody who still knows their password carries on as though this never happened,
// which is what makes it safe for an administrator to hand one over unprompted.
func (s *Store) CreateRecoveryLink(ctx context.Context, principalID, createdBy string) (*RecoveryLink, string, error) {
	// Read first, so a link cannot be minted against an id that is not an account — the row
	// would insert against the foreign key and then sit there naming nothing.
	p, err := s.PrincipalByID(ctx, principalID)
	if err != nil {
		return nil, "", err
	}
	// A disabled account cannot sign in whatever its password is, so a link into it leads
	// nowhere. Refusing here says so, rather than letting somebody set a password and find
	// out at the login form.
	if p.Disabled() {
		return nil, "", Conflict("that account is disabled, so a new password would not let anybody in")
	}

	buf := make([]byte, recoveryTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", fmt.Errorf("read random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	now := s.Now()
	link := &RecoveryLink{
		ID:          ids.New(ids.RecoveryLink),
		PrincipalID: principalID,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		ExpiresAt:   now.Add(RecoveryLinkTTL),
	}

	// NULL rather than the empty string when nobody issued it: the column is a reference,
	// and "" is not an account.
	var author any
	if createdBy != "" {
		author = createdBy
	}
	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO recovery_links (id, token_hash, principal_id, created_by, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		link.ID, hashToken(token), link.PrincipalID, author,
		unix(link.CreatedAt), unix(link.ExpiresAt)); err != nil {
		return nil, "", fmt.Errorf("create recovery link: %w", err)
	}
	return link, token, nil
}

const recoveryLinkColumns = `id, principal_id, created_by, created_at, expires_at, used_at, voided_at`

func scanRecoveryLink(row interface{ Scan(...any) error }) (*RecoveryLink, error) {
	var (
		l         RecoveryLink
		createdBy sql.NullString
		created   int64
		expires   int64
		used      sql.NullInt64
		voided    sql.NullInt64
	)
	if err := row.Scan(&l.ID, &l.PrincipalID, &createdBy, &created, &expires, &used, &voided); err != nil {
		return nil, err
	}
	l.CreatedBy = createdBy.String
	l.CreatedAt = time.Unix(created, 0).UTC()
	l.ExpiresAt = time.Unix(expires, 0).UTC()
	l.UsedAt = timeFrom(used)
	l.VoidedAt = timeFrom(voided)
	return &l, nil
}

// RecoveryLinkByToken looks a link up by the token in it.
//
// Spent, voided and expired links come back like usable ones. The page has to tell those
// apart to say anything useful — wait, ask again, or just sign in — and which state a token
// is in is not a secret from whoever is holding that token.
func (s *Store) RecoveryLinkByToken(ctx context.Context, token string) (*RecoveryLink, error) {
	link, err := scanRecoveryLink(s.main.QueryRowContext(ctx,
		`SELECT `+recoveryLinkColumns+` FROM recovery_links WHERE token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("that recovery link is not one of ours")
	}
	return link, err
}

// UseRecoveryLink spends a link on a new password, and returns the account it let into.
//
// One transaction, so a token cannot set two passwords however many times it is submitted
// and a password cannot move without the link that moved it being stamped.
//
// Three things happen together, and each is the point of one of the others:
//
//   - the password is replaced, which is what somebody came here for;
//   - every session for the account ends, because the likeliest reason to be here at all is
//     that somebody else has one, and leaving them signed in would make the whole exercise
//     decorative. Unlike changing a password from inside the application there is no session
//     to keep — whoever did this is at a form, not signed in;
//   - every other outstanding link for the account is voided, because from the moment one is
//     spent the rest are indistinguishable from stolen ones. A link that was legitimately in
//     somebody's inbox and an old one an attacker kept look exactly alike, and the account
//     has just demonstrated it does not need either.
func (s *Store) UseRecoveryLink(ctx context.Context, token, password string) (*Principal, error) {
	// Before the transaction and before bcrypt: hashing at cost 12 on an endpoint anybody
	// can reach is a reason not to reach it with nonsense.
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	link, err := scanRecoveryLink(tx.QueryRowContext(ctx,
		`SELECT `+recoveryLinkColumns+` FROM recovery_links WHERE token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("that recovery link is not one of ours")
	}
	if err != nil {
		return nil, err
	}

	now := s.Now()
	switch {
	case link.Used():
		return nil, Conflict("that recovery link has already been used")
	case link.Voided():
		return nil, Conflict("that recovery link was replaced when a newer one was used")
	case link.Expired(now):
		return nil, Invalid("that recovery link expired on %s; ask for a new one",
			link.ExpiresAt.Format(time.DateOnly))
	}

	p, err := scanPrincipal(tx.QueryRowContext(ctx,
		`SELECT `+principalColumns+` FROM principals WHERE id = ?`, link.PrincipalID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("that account is gone")
	}
	if err != nil {
		return nil, err
	}
	// Checked again here rather than only at issue: an account can be disabled in the day
	// between a link being handed out and being opened.
	if p.Disabled() {
		return nil, Conflict("that account is disabled, so a new password would not let anybody in")
	}

	hashed, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE principals SET password_hash = ? WHERE id = ?`, hashed, p.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE principal_id = ?`, p.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE recovery_links SET used_at = ? WHERE id = ?`, unix(now), link.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE recovery_links SET voided_at = ?
		 WHERE principal_id = ? AND id <> ? AND used_at IS NULL AND voided_at IS NULL`,
		unix(now), p.ID, link.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// VoidRecoveryLink takes a link out of use without anybody having spent it.
//
// For the one case where a link exists and its only copy does not: a mail that failed to
// leave. Left alone that row would sit there outstanding, keeping the account's real link
// voided the moment somebody used it — and doing nothing for anybody, because the token went
// nowhere.
//
// Silent about a link that is already used or voided. This is called to reach a state, not to
// perform an operation, and the state is already reached.
func (s *Store) VoidRecoveryLink(ctx context.Context, id string) error {
	_, err := s.main.ExecContext(ctx, `
		UPDATE recovery_links SET voided_at = ?
		 WHERE id = ? AND used_at IS NULL AND voided_at IS NULL`, unix(s.Now()), id)
	return err
}
