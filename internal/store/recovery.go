package store

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"
)

// CodeTTL is how long a confirmation code is good for.
//
// Long enough to go and find the mail in another tab, short enough that a code left sitting
// in an inbox is not a standing offer. Somebody who takes longer starts again, which costs
// one click.
const CodeTTL = 15 * time.Minute

// MaxRecoveryAttempts is how many wrong codes before the attempt is thrown away.
//
// Bounded attempts are what make a code this short safe. Discarded rather than locked:
// a lockout is a state somebody has to wait out, and starting again is faster and no weaker.
const MaxRecoveryAttempts = 5

// codeLen is eight characters — forty bits.
//
// Not a key. It is one guess in a trillion, bounded to five attempts, valid for a quarter
// of an hour, and it authorises nothing except the address it was sent to.
const codeLen = 8

// codeAlphabet is Crockford's base32, the same as an id's: no I, L, O or U.
//
// This one is read off a screen and typed into another, sometimes off a phone. A glyph
// that cannot be told from another one is a support request.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// BeginRecovery starts proving an address and hands back the code to send there.
//
// Replaces any attempt already in flight. Starting again is what somebody does when the
// mail did not arrive, and two live codes for one account is two chances at the same guess.
//
// The row is written before the caller sends anything, so a send that fails leaves a code
// nobody has — which expires. Writing it afterwards would lose the code if the write failed
// and leave somebody holding one this program has never heard of.
func (s *Store) BeginRecovery(ctx context.Context, principalID, email string) (string, error) {
	address, err := checkAddress(email)
	if err != nil {
		return "", err
	}

	code, err := newCode()
	if err != nil {
		return "", err
	}

	now := time.Now()
	_, err = s.main.ExecContext(ctx, `
		INSERT INTO recovery_pending
			(principal_id, email, code_hash, attempts, expires_at, created_at)
		VALUES (?, ?, ?, 0, ?, ?)
		ON CONFLICT (principal_id) DO UPDATE SET
			email = excluded.email,
			code_hash = excluded.code_hash,
			attempts = 0,
			expires_at = excluded.expires_at,
			created_at = excluded.created_at`,
		principalID, address, hashToken(code), now.Add(CodeTTL).Unix(), now.Unix())
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConfirmRecovery finishes proving an address, or refuses. Returns the address now on record.
//
// One refusal for wrong, expired, exhausted and absent alike. Which of the four it was tells
// a caller something about an account that may not be theirs, and tells the account's owner
// nothing they could not work out by trying again.
func (s *Store) ConfirmRecovery(ctx context.Context, principalID, code string) (string, string, error) {
	refused := Invalid("that code is wrong or has expired")

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()

	var (
		email    string
		expected []byte
		attempts int
		expires  int64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT email, code_hash, attempts, expires_at
		  FROM recovery_pending WHERE principal_id = ?`, principalID).
		Scan(&email, &expected, &attempts, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", refused
	}
	if err != nil {
		return "", "", err
	}

	if time.Now().Unix() >= expires || attempts >= MaxRecoveryAttempts {
		// Cleared rather than left to rot: the next attempt starts from nothing, and an
		// expired row that stays is a row somebody has to reason about later.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM recovery_pending WHERE principal_id = ?`, principalID); err != nil {
			return "", "", err
		}
		if err := tx.Commit(); err != nil {
			return "", "", err
		}
		return "", "", refused
	}

	// Constant-time, because this is a secret being compared and an early exit on the first
	// differing byte is an oracle. Upper-cased first: the code is typed by hand, and a
	// person who types it in lower case has typed the right code.
	got := hashToken(strings.ToUpper(strings.TrimSpace(code)))
	if subtle.ConstantTimeCompare(got, expected) != 1 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE recovery_pending SET attempts = attempts + 1 WHERE principal_id = ?`,
			principalID); err != nil {
			return "", "", err
		}
		if err := tx.Commit(); err != nil {
			return "", "", err
		}
		return "", "", refused
	}

	displaced, err := takeOverAddress(ctx, tx, principalID, email)
	if err != nil {
		return "", "", err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_recovery (principal_id, email, confirmed_at)
		VALUES (?, ?, ?)
		ON CONFLICT (principal_id) DO UPDATE SET
			email = excluded.email, confirmed_at = excluded.confirmed_at`,
		principalID, email, time.Now().Unix()); err != nil {
		return "", "", err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM recovery_pending WHERE principal_id = ?`, principalID); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return email, displaced, nil
}

// takeOverAddress moves an address away from whichever account had it, and says which.
//
// The last account to prove an address is the one it belongs to. Whoever can read that inbox
// today is who recovery through it would actually reach, and pretending otherwise keeps an
// address pointed at somebody who has since lost it — a work address reassigned, a shared one
// somebody moved out of. Refusing instead would refuse the person who really can read it and
// leave the one who cannot on record, which is the wrong way round.
//
// This gives up nothing. Anybody who can prove control of that inbox could already recover
// the account attached to it; proving it here reaches no further.
//
// The real cost is that the displaced account loses its recovery address without being told.
// There is nowhere to tell them — that would be a mail to the address they just lost — so
// the caller logs it instead.
func takeOverAddress(ctx context.Context, tx *sql.Tx, principalID, email string) (string, error) {
	var displaced string
	err := tx.QueryRowContext(ctx, `
		SELECT principal_id FROM user_recovery
		 WHERE email = ? COLLATE NOCASE AND principal_id <> ?`, email, principalID).Scan(&displaced)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	// Deleted rather than left for the unique index to refuse. The index is still what makes
	// one-account-per-address true; this is what stops the rule being enforced against the
	// person who just proved they are the right account for it.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery WHERE principal_id = ?`, displaced); err != nil {
		return "", err
	}
	return displaced, nil
}

// RecoveryEmail is the proved address, or empty. Never the pending one — that is not on
// record, and the whole point of the two tables is that nothing can mistake it for one.
func (s *Store) RecoveryEmail(ctx context.Context, principalID string) (string, error) {
	var email string
	err := s.main.QueryRowContext(ctx,
		`SELECT email FROM user_recovery WHERE principal_id = ?`, principalID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return email, err
}

// PendingRecovery is the address partway through being proved, if there is one.
//
// So a page reopened mid-flow can say which address it is waiting on, rather than starting
// somebody over on a code they are already holding.
func (s *Store) PendingRecovery(ctx context.Context, principalID string) (string, error) {
	var email string
	err := s.main.QueryRowContext(ctx, `
		SELECT email FROM recovery_pending
		 WHERE principal_id = ? AND expires_at > ?`, principalID, time.Now().Unix()).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return email, err
}

// ClearPendingRecovery drops an attempt in flight, leaving any proved address alone.
//
// Its own method rather than a flag on [Store.ClearRecovery], because the difference is the
// whole point: somebody changing an address they already have must not lose the one that
// works because the new one could not be reached.
func (s *Store) ClearPendingRecovery(ctx context.Context, principalID string) error {
	_, err := s.main.ExecContext(ctx,
		`DELETE FROM recovery_pending WHERE principal_id = ?`, principalID)
	return err
}

// ClearRecovery forgets the address and anything in flight.
func (s *Store) ClearRecovery(ctx context.Context, principalID string) error {
	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery WHERE principal_id = ?`, principalID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM recovery_pending WHERE principal_id = ?`, principalID); err != nil {
		return err
	}
	return tx.Commit()
}

// newCode is eight characters somebody can read off one screen and type into another.
func newCode() (string, error) {
	buf := make([]byte, codeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, codeLen)
	for i, b := range buf {
		// The alphabet is exactly 32 long, so five bits map to one character with nothing
		// left over: no character is more likely than another and no rejection loop is
		// needed to keep it that way.
		out[i] = codeAlphabet[int(b)%32]
	}
	return string(out), nil
}

// checkAddress refuses something that is plainly not an address.
//
// Deliberately shallow, and net/mail rather than a pattern of our own. Whether mail arrives
// is the relay's answer, and a stricter rule here would refuse addresses that work — the
// local part of an address may contain almost anything. What this catches is a typo bad
// enough that sending would be pointless.
func checkAddress(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", Invalid("an address is required")
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", Invalid("%q is not a usable address", trimmed)
	}
	// ParseAddress accepts "Name <a@b>"; only the address itself is ever sent to. It also
	// accepts a bare "a@b", which no relay will deliver — a dot in the domain is the one
	// extra thing worth insisting on.
	_, domain, _ := strings.Cut(parsed.Address, "@")
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") {
		return "", Invalid("%q is not a usable address", trimmed)
	}
	return parsed.Address, nil
}
