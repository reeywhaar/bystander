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

	// Email is the address this invitation was sent to, or empty for one handed over.
	//
	// It is what makes accepting an emailed invitation *proof* of an address: the link went
	// there and nowhere else, so whoever used it read that inbox. AcceptInvite binds it as
	// the new account's recovery address on the strength of that.
	Email string
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
// email is the address it will be sent to, or empty for a link to be handed over. It is kept
// so that accepting the invitation can bind it as a proved recovery address — see Invite.Email
// — and it is normalised the same way a recovery address is, because the two end up in the
// same column and a difference in spelling would be a difference in identity.
func (s *Store) CreateInvite(ctx context.Context, role Role, createdBy, email string) (*Invite, string, error) {
	if !role.Valid() {
		return nil, "", Invalid("%q is not a role", role)
	}
	if email != "" {
		address, err := checkAddress(email)
		if err != nil {
			return nil, "", err
		}
		email = address
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
		Email:     email,
	}

	// A NULL created_by is the bootstrap invitation: nobody issued it, the program did.
	var creator any
	if createdBy != "" {
		creator = createdBy
	}
	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO invites (id, token_hash, role, created_by, created_at, expires_at, email)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, hashToken(token), string(role), creator,
		unix(inv.CreatedAt), unix(inv.ExpiresAt), inv.Email); err != nil {
		return nil, "", fmt.Errorf("create invite: %w", err)
	}
	return inv, token, nil
}

const inviteColumns = `id, role, created_by, created_at, expires_at, accepted_at, principal_id, email`

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
	if err := row.Scan(&inv.ID, &role, &createdBy, &created, &expires, &accepted, &principal, &inv.Email); err != nil {
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
// cannot exist whose invitation still reads as outstanding. An emailed invitation's address
// is bound in the same one, for the same reason — an account either exists with the recovery
// address it was promised or does not exist.
//
// The second return is the account whose recovery address this one took over, if any, for the
// caller to log. There is nowhere to tell them: that would be a mail to the address they just
// lost. Same rule and same reason as proving an address with a code — see takeOverAddress.
func (s *Store) AcceptInvite(ctx context.Context, token, username, password string) (*Principal, string, error) {
	// Validate before opening a transaction and before hashing: bcrypt at cost 12 on an
	// endpoint anybody can reach is a reason not to reach it with nonsense.
	if err := ValidateUsername(username); err != nil {
		return nil, "", err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, "", err
	}

	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	inv, err := scanInvite(tx.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", NotFound("that invitation link is not one of ours")
	}
	if err != nil {
		return nil, "", err
	}

	now := s.Now()
	switch {
	case inv.Accepted():
		return nil, "", Conflict("that invitation has already been used")
	case inv.Expired(now):
		return nil, "", Invalid("that invitation expired on %s; ask for a new link", inv.ExpiresAt.Format(time.DateOnly))
	}

	hashed, err := hashPassword(password)
	if err != nil {
		return nil, "", err
	}
	p, err := insertPrincipal(ctx, tx, &Principal{
		ID:        ids.New(ids.Principal),
		Username:  username,
		Role:      inv.Role,
		CreatedAt: now,
		hash:      hashed,
	}, now)
	if err != nil {
		return nil, "", err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE invites SET accepted_at = ?, principal_id = ? WHERE id = ?`,
		unix(now), p.ID, inv.ID); err != nil {
		return nil, "", err
	}

	// An emailed invitation proves its address, so the account starts with a recovery address
	// already on it rather than being asked to prove the same inbox a second time with a code.
	// The proof is the same one a code gives — somebody read that inbox — obtained from a mail
	// that had to be sent anyway.
	//
	// In this transaction, so an account either exists with its address or does not exist. A
	// second write afterwards could fail and leave somebody signed in believing they have a
	// recovery address, which is the belief this whole area exists to make true.
	var displaced string
	if inv.Email != "" {
		// Same rule as proving an address by code: the last account to prove it holds it.
		// Whoever can read that inbox today is who recovery through it would reach.
		taken, err := takeOverAddress(ctx, tx, p.ID, inv.Email)
		if err != nil {
			return nil, "", err
		}
		displaced = taken
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_recovery (principal_id, email, confirmed_at) VALUES (?, ?, ?)`,
			p.ID, inv.Email, unix(now)); err != nil {
			return nil, "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return p, displaced, nil
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
