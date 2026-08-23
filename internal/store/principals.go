package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"bystander/internal/ids"
)

// Role is what an account may do. There are two, and there is no plan for a third.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// Valid reports whether r is a role this program knows.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleUser }

// bcryptCost is deliberately above bcrypt.DefaultCost (10). A login here happens a few
// times a week, so ~250ms is invisible to the person logging in and expensive to somebody
// working through a stolen database.
const bcryptCost = 12

// Password bounds. The maximum is not arbitrary: bcrypt hashes only the first 72 bytes of
// its input, so anything longer is silently truncated — which would mean two different
// passwords that both work. Refusing is honest; truncating is a trap.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 72
)

// Username bounds.
const (
	MinUsernameLen = 2
	MaxUsernameLen = 32
)

// Principal is an account.
type Principal struct {
	ID         string
	Username   string
	Role       Role
	CreatedAt  time.Time
	DisabledAt time.Time // zero when enabled

	// hash is the bcrypt hash. Unexported so it cannot be serialised into a response by
	// somebody adding a json tag to a struct they did not read to the bottom of.
	hash string
}

// Disabled reports whether this account has been switched off.
func (p *Principal) Disabled() bool { return !p.DisabledAt.IsZero() }

// CreatePrincipal makes an account and the settings row that belongs to it.
//
// Both in one transaction, so the scheduler never meets a principal whose settings are
// missing and no code downstream has to cope with that absence.
func (s *Store) CreatePrincipal(ctx context.Context, username, password string, role Role) (*Principal, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, Invalid("%q is not a role", role)
	}

	hashed, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	now := s.Now()
	p := &Principal{
		ID:        ids.New(ids.Principal),
		Username:  username,
		Role:      role,
		CreatedAt: now,
		hash:      hashed,
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	p, err = insertPrincipal(ctx, tx, p, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// insertPrincipal writes an account and the settings row that belongs to it.
//
// Both in one transaction, so the scheduler never meets a principal whose settings are
// missing and nothing downstream has to cope with that absence. Takes a transaction rather
// than opening one because accepting an invitation has to stamp the invite in the same
// breath — see AcceptInvite.
func insertPrincipal(ctx context.Context, tx *sql.Tx, p *Principal, now time.Time) (*Principal, error) {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO principals (id, username, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Username, p.hash, string(p.Role), unix(now)); err != nil {
		if isUnique(err) {
			return nil, Conflict("the name %q is taken", p.Username)
		}
		return nil, fmt.Errorf("create principal: %w", err)
	}

	// next_edition_at is now, not now+interval. A brand new account has no feeds, and the
	// generator skips a principal with nothing to draw from without moving the clock — so
	// the first real edition arrives on the tick after they add a feed, rather than a day
	// later.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO settings (principal_id, next_edition_at) VALUES (?, ?)`,
		p.ID, unix(now)); err != nil {
		return nil, fmt.Errorf("create settings: %w", err)
	}
	return p, nil
}

const principalColumns = `id, username, password_hash, role, created_at, disabled_at`

func scanPrincipal(row interface{ Scan(...any) error }) (*Principal, error) {
	var (
		p        Principal
		role     string
		created  int64
		disabled sql.NullInt64
	)
	if err := row.Scan(&p.ID, &p.Username, &p.hash, &role, &created, &disabled); err != nil {
		return nil, err
	}
	p.Role = Role(role)
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.DisabledAt = timeFrom(disabled)
	return &p, nil
}

// PrincipalByID returns one account.
func (s *Store) PrincipalByID(ctx context.Context, id string) (*Principal, error) {
	p, err := scanPrincipal(s.main.QueryRowContext(ctx,
		`SELECT `+principalColumns+` FROM principals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no account %s", id)
	}
	return p, err
}

// PrincipalByUsername returns one account. The lookup is case-insensitive, because the
// column is.
func (s *Store) PrincipalByUsername(ctx context.Context, username string) (*Principal, error) {
	p, err := scanPrincipal(s.main.QueryRowContext(ctx,
		`SELECT `+principalColumns+` FROM principals WHERE username = ?`, strings.TrimSpace(username)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no account named %q", username)
	}
	return p, err
}

// ListPrincipals returns every account, newest first — which is the end somebody just
// added to, and therefore the end they are looking for.
func (s *Store) ListPrincipals(ctx context.Context) ([]*Principal, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+principalColumns+` FROM principals ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Principal
	for rows.Next() {
		p, err := scanPrincipal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountAdmins counts the enabled administrators. Used to refuse the deletion that would
// lock everybody out.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.main.QueryRowContext(ctx,
		`SELECT count(*) FROM principals WHERE role = 'admin' AND disabled_at IS NULL`).Scan(&n)
	return n, err
}

// Authenticate checks a password and returns the account it belongs to.
//
// A wrong name and a wrong password produce the same error, deliberately: distinguishing
// them turns the login form into a list of who has an account here. The bcrypt comparison
// runs even when no such account exists, so the two cases take the same time as well as
// saying the same thing.
func (s *Store) Authenticate(ctx context.Context, username, password string) (*Principal, error) {
	refused := NotFound("that name and password do not match an account")

	p, err := s.PrincipalByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// A hash of the right shape and cost over a value nothing will match, so the
			// timing of an unknown username matches that of a wrong password.
			bcrypt.CompareHashAndPassword([]byte(decoyHash), []byte(password))
			return nil, refused
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(p.hash), []byte(password)) != nil {
		return nil, refused
	}
	if p.Disabled() {
		return nil, Invalid("that account is disabled")
	}
	return p, nil
}

// decoyHash is bcrypt cost 12 over a value nobody holds. Its only job is to cost the same
// as a real comparison.
const decoyHash = "$2a$12$C6UzMDM.H6dfI/f/IKcEe.Bx3H0aiTUKKD5C2A6Vp5DDkiT9AR/Wm"

// SetPassword replaces an account's password and ends every session it opened.
//
// One transaction, because "changing my password signs me out everywhere" is only true if
// the two cannot come apart. Doing it here rather than leaving it to the caller means no
// caller can forget — including the CLI, which runs in a different process and could not
// reach an in-memory session table at all.
func (s *Store) SetPassword(ctx context.Context, id, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE principals SET password_hash = ? WHERE id = ?`, hashed, id)
	if err != nil {
		return err
	}
	if err := expectOne(res, NotFound("no account %s", id)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE principal_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetDisabled switches an account off or back on.
//
// Disabling does not touch their feeds. An account that is re-enabled should find its
// subscriptions where it left them.
func (s *Store) SetDisabled(ctx context.Context, id string, disabled bool) error {
	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE principals SET disabled_at = ? WHERE id = ?`, nullTime(disabledAt(s.Now(), disabled)), id)
	if err != nil {
		return err
	}
	if err := expectOne(res, NotFound("no account %s", id)); err != nil {
		return err
	}
	// Switching an account off has to take effect now, not whenever its cookie happens to
	// lapse. Re-enabling deliberately does not restore sessions: they were ended.
	if disabled {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE principal_id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func disabledAt(now time.Time, disabled bool) time.Time {
	if disabled {
		return now
	}
	return time.Time{}
}

// DeletePrincipal removes an account. Sessions, invitations, tags and subscriptions go
// with it by cascade; the editions and items in derived.db are collected by the sweep,
// because no constraint can cross a database.
func (s *Store) DeletePrincipal(ctx context.Context, id string) error {
	res, err := s.main.ExecContext(ctx, `DELETE FROM principals WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no account %s", id))
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// ValidateUsername is the one definition of an acceptable name.
//
// Letters, digits, and the three separators people actually type. No spaces, because a
// name with a trailing space is a name nobody can log in with and nobody can see why.
func ValidateUsername(name string) error {
	if n := len([]rune(name)); n < MinUsernameLen || n > MaxUsernameLen {
		return Invalid("a name is between %d and %d characters", MinUsernameLen, MaxUsernameLen)
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '-', r == '_', r == '.':
		default:
			return Invalid("a name may hold letters, digits, and - _ . — %q is not one of those", r)
		}
	}
	if r := []rune(name)[0]; !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return Invalid("a name starts with a letter or a digit")
	}
	return nil
}

// ValidatePassword enforces the bounds bcrypt imposes and one of our own.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen {
		return Invalid("a password is at least %d characters", MinPasswordLen)
	}
	// Bytes, not runes: bcrypt's limit is on bytes, so a passphrase of emoji hits it
	// sooner than its length suggests.
	if len(password) > MaxPasswordLen {
		return Invalid("a password is at most %d bytes", MaxPasswordLen)
	}
	return nil
}
