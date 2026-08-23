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

// InviteTTL is how long a link stays usable. Long enough to survive a weekend, short
// enough that a link forgotten in a chat log is not a way in a month later.
const InviteTTL = 7 * 24 * time.Hour

// inviteTokenBytes is the entropy in an invitation token. 32 bytes is 256 bits, which is
// the same standard the session id is held to and for the same reason: it is a bearer
// credential that creates an account.
const inviteTokenBytes = 32

// Invite is an unaccepted, or once-accepted, invitation.
//
// An accepted invitation keeps its row and points at the principal it produced. That is
// the record of where an account came from, and it is why accepting stamps AcceptedAt
// rather than deleting.
type Invite struct {
	ID          string
	Role        Role
	CreatedBy   string // empty once its creator is deleted
	CreatedAt   time.Time
	ExpiresAt   time.Time
	AcceptedAt  time.Time // zero while outstanding
	PrincipalID string    // the account it produced, once accepted
}

// Accepted reports whether this invitation has already been used.
func (i *Invite) Accepted() bool { return !i.AcceptedAt.IsZero() }

// Expired reports whether this invitation has lapsed.
func (i *Invite) Expired(now time.Time) bool { return now.After(i.ExpiresAt) }

// Usable reports whether this invitation can still create an account.
func (i *Invite) Usable(now time.Time) bool { return !i.Accepted() && !i.Expired(now) }

// CreateInvite mints an invitation and returns it together with its token.
//
// The token is returned exactly once, here. What is stored is its hash, so a link that
// gets lost is reissued rather than recovered — the same stance as sessions, and for the
// same reason: nothing in the database should be replayable by whoever can read it.
func (s *Store) CreateInvite(ctx context.Context, role Role, createdBy string) (*Invite, string, error) {
	if !role.Valid() {
		return nil, "", Invalid("%q is not a role", role)
	}

	buf := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", fmt.Errorf("read random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	now := s.Now()
	inv := &Invite{
		ID:        ids.New(ids.Invite),
		Role:      role,
		CreatedBy: createdBy,
		CreatedAt: now,
		ExpiresAt: now.Add(InviteTTL),
	}

	// A NULL created_by is the bootstrap invitation: nobody issued it, the program did.
	var creator any
	if createdBy != "" {
		creator = createdBy
	}
	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO invites (id, token_hash, role, created_by, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		inv.ID, hashToken(token), string(role), creator, unix(inv.CreatedAt), unix(inv.ExpiresAt)); err != nil {
		return nil, "", fmt.Errorf("create invite: %w", err)
	}
	return inv, token, nil
}

const inviteColumns = `id, role, created_by, created_at, expires_at, accepted_at, principal_id`

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var (
		inv       Invite
		role      string
		createdBy sql.NullString
		created   int64
		expires   int64
		accepted  sql.NullInt64
		principal sql.NullString
	)
	if err := row.Scan(&inv.ID, &role, &createdBy, &created, &expires, &accepted, &principal); err != nil {
		return nil, err
	}
	inv.Role = Role(role)
	inv.CreatedBy = createdBy.String
	inv.CreatedAt = time.Unix(created, 0).UTC()
	inv.ExpiresAt = time.Unix(expires, 0).UTC()
	inv.AcceptedAt = timeFrom(accepted)
	inv.PrincipalID = principal.String
	return &inv, nil
}

// InviteByToken looks an invitation up by the token in a link.
//
// It returns invitations that are expired or already accepted as well as usable ones. The
// acceptance page needs to tell those three states apart to say anything useful, and
// which state a token is in is not a secret from whoever is holding that token.
func (s *Store) InviteByToken(ctx context.Context, token string) (*Invite, error) {
	inv, err := scanInvite(s.main.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("that invitation link is not one of ours")
	}
	return inv, err
}

// ListInvites returns every invitation, newest first.
func (s *Store) ListInvites(ctx context.Context) ([]*Invite, error) {
	rows, err := s.main.QueryContext(ctx, `SELECT `+inviteColumns+` FROM invites ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// AcceptInvite turns an invitation into an account.
//
// One transaction: the account and the stamp on the invitation cannot come apart, so a
// token cannot create two accounts however many times it is submitted, and an account
// cannot exist whose invitation still reads as outstanding.
func (s *Store) AcceptInvite(ctx context.Context, token, username, password string) (*Principal, error) {
	// Validate before opening a transaction and before hashing: bcrypt at cost 12 on an
	// endpoint anybody can reach is a reason not to reach it with nonsense.
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	inv, err := scanInvite(tx.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("that invitation link is not one of ours")
	}
	if err != nil {
		return nil, err
	}

	now := s.Now()
	switch {
	case inv.Accepted():
		return nil, Conflict("that invitation has already been used")
	case inv.Expired(now):
		return nil, Invalid("that invitation expired on %s; ask for a new link", inv.ExpiresAt.Format(time.DateOnly))
	}

	hashed, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	p, err := insertPrincipal(ctx, tx, &Principal{
		ID:        ids.New(ids.Principal),
		Username:  username,
		Role:      inv.Role,
		CreatedAt: now,
		hash:      hashed,
	}, now)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE invites SET accepted_at = ?, principal_id = ? WHERE id = ?`,
		unix(now), p.ID, inv.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteInvite withdraws an outstanding invitation.
//
// Refused for one already accepted: that row is the record of where an account came from,
// and deleting it would erase the answer to "who let this person in". Disable the account
// instead.
func (s *Store) DeleteInvite(ctx context.Context, id string) error {
	inv, err := scanInvite(s.main.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no invitation %s", id)
	}
	if err != nil {
		return err
	}
	if inv.Accepted() {
		return Conflict("that invitation has been accepted; disable the account instead")
	}
	res, err := s.main.ExecContext(ctx, `DELETE FROM invites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no invitation %s", id))
}
