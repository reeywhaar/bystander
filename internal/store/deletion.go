package store

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// DeletionGrace is how long an account waits between being asked for and being erased.
//
// A week, because the point of the wait is that somebody who did not mean it gets it back,
// and the two ways that happens — noticing, or being told by the message that went to their
// recovery address — both take a person rather than a machine. A day would not survive a
// holiday; a month would make "delete my account" mean "eventually, probably".
const DeletionGrace = 7 * 24 * time.Hour

// ScheduleDeletion marks an account to be erased, and ends every session it had.
//
// The password is required, exactly as it is to change one. Being signed in is not the same
// as knowing it, and the difference is what stops a borrowed session becoming a destroyed
// account. Note that this is the *weaker* of the two protections: the stronger one is that
// signing in calls the deletion off, so even a successful one is undone by the owner doing
// the ordinary thing within the week.
//
// Sessions end for the same reason disabling ends them — the request should take effect now
// rather than whenever a cookie happens to lapse — and here it is also what makes the next
// sign-in a deliberate act rather than a tab left open quietly withdrawing the request.
//
// Returns when the request was recorded. The caller adds [DeletionGrace] to it rather than
// reading a clock of its own — two reads either side of a second boundary would have the
// answer disagree with the row by a second, which is small and is still two truths.
func (s *Store) ScheduleDeletion(ctx context.Context, id, password string) (time.Time, error) {
	p, err := s.PrincipalByID(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(p.hash), []byte(password)) != nil {
		return time.Time{}, Invalid("that is not your password")
	}
	// Already asked. Answering with the date already set rather than pushing it back, so
	// pressing the button twice does not quietly buy another week.
	if p.ScheduledForDeletion() {
		return p.DeletedAt, nil
	}

	now := s.Now()
	tx, err := s.main.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE principals SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
		unix(now), id)
	if err != nil {
		return time.Time{}, fmt.Errorf("schedule deletion: %w", err)
	}
	if err := expectOne(res, NotFound("no account %s", id)); err != nil {
		return time.Time{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE principal_id = ?`, id); err != nil {
		return time.Time{}, fmt.Errorf("schedule deletion: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

// CancelDeletion withdraws a request to be erased, and reports whether there was one.
//
// Called on every successful sign-in, which is why it has to be cheap on the ordinary path:
// the UPDATE touches nothing when nothing was scheduled, and the boolean comes from the
// rows it affected rather than from a read first.
func (s *Store) CancelDeletion(ctx context.Context, id string) (bool, error) {
	res, err := s.main.ExecContext(ctx,
		`UPDATE principals SET deleted_at = NULL, deletion_cancelled_at = ?
		  WHERE id = ? AND deleted_at IS NOT NULL`,
		unix(s.Now()), id)
	if err != nil {
		return false, fmt.Errorf("cancel deletion: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeletedAccount is one account the purge is about to erase, named for the log.
//
// The id and the username, because after this there is nothing left to look either up in.
// An erasure that leaves no trace anywhere is indistinguishable from a bug that lost an
// account, and an operator asked "where did this person go" needs an answer.
type DeletedAccount struct {
	ID       string
	Username string
}

// PurgeDeletedAccounts erases every account whose grace period has run out.
//
// What goes is what belongs to the person. In main.db that is everything hanging off the
// principal by cascade: sessions, tags, subscriptions, pages and their filters, the recovery
// address and anything in flight, shared links. Invitations keep their row and lose their
// pointer, because who let somebody in is a fact about the invitation and outlives the
// account it made — see `20260825235815_main_invite_survives_its_account`.
//
// What stays is what belongs to everybody. **Feeds and the articles in them are not the
// person's**: they are held once and shared by everyone who follows them, and a subscription
// is the only part of that relationship somebody owns. Erasing an account takes the
// subscription and leaves the feed, so one person leaving cannot take another person's
// reading with them. A feed that had no other follower is collected later by the ordinary
// orphan sweep, on the same terms as unsubscribing from it by hand — which is what it is.
//
// derived.db is done explicitly rather than left to that sweep. No constraint crosses a
// database, so the editions, what was shown and what was read have no cascade to travel
// along — and an erasure that depends on a garbage collector running afterwards is an
// erasure that has not happened yet. The sweep still collects orphans; it is the safety net
// here, not the mechanism.
//
// main.db goes first on purpose. Interrupted in between, the account is gone and what is
// left in derived.db is exactly the orphan the sweep already knows how to collect. The other
// order would leave an account whose history had been erased under it.
func (s *Store) PurgeDeletedAccounts(ctx context.Context, grace time.Duration) ([]DeletedAccount, error) {
	cutoff := s.Now().Add(-grace)

	rows, err := s.main.QueryContext(ctx,
		`SELECT id, username FROM principals WHERE deleted_at IS NOT NULL AND deleted_at <= ?`,
		unix(cutoff))
	if err != nil {
		return nil, fmt.Errorf("purge deleted accounts: %w", err)
	}
	defer rows.Close()

	var due []DeletedAccount
	for rows.Next() {
		var account DeletedAccount
		if err := rows.Scan(&account.ID, &account.Username); err != nil {
			return nil, fmt.Errorf("purge deleted accounts: %w", err)
		}
		due = append(due, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("purge deleted accounts: %w", err)
	}
	rows.Close()

	// One at a time rather than one statement, so a row that will not go — a constraint
	// nobody anticipated — takes itself out of the pass rather than the rest of them.
	erased := make([]DeletedAccount, 0, len(due))
	for _, account := range due {
		// Read before the delete, because a moment later there is no principal to find
		// them from: pages cascade with their owner, and the editions keyed to them live
		// in the other database with no constraint to follow.
		pages, err := s.pageIDsOf(ctx, account.ID)
		if err != nil {
			return erased, err
		}
		if _, err := s.main.ExecContext(ctx,
			`DELETE FROM principals WHERE id = ?`, account.ID); err != nil {
			return erased, fmt.Errorf("purge account %s: %w", account.ID, err)
		}
		if err := s.purgeDerived(ctx, account.ID, pages); err != nil {
			return erased, err
		}
		erased = append(erased, account)
	}
	return erased, nil
}

// pageIDsOf is one account's pages, for the rows in the other database that are keyed to them.
func (s *Store) pageIDsOf(ctx context.Context, principalID string) ([]string, error) {
	rows, err := s.main.QueryContext(ctx, `SELECT id FROM pages WHERE principal_id = ?`, principalID)
	if err != nil {
		return nil, fmt.Errorf("purge account %s: %w", principalID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("purge account %s: %w", principalID, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// purgeDerived removes one erased account's half of derived.db.
//
// Only its own. `items` is not touched and neither is anything about a feed: those are held
// once for the whole instance, and the rows here are the three that name a person or one of
// their pages.
func (s *Store) purgeDerived(ctx context.Context, principalID string, pageIDs []string) error {
	// Edition items go with their edition by cascade — same database, so there is one.
	if len(pageIDs) > 0 {
		args, marks := inList(pageIDs)
		for _, table := range []string{"editions", "shown"} {
			if _, err := s.derived.ExecContext(ctx,
				`DELETE FROM `+table+` WHERE page_id IN (`+marks+`)`, args...); err != nil {
				return fmt.Errorf("purge %s of account %s: %w", table, principalID, err)
			}
		}
	}
	if _, err := s.derived.ExecContext(ctx,
		`DELETE FROM read_articles WHERE principal_id = ?`, principalID); err != nil {
		return fmt.Errorf("purge read articles of account %s: %w", principalID, err)
	}
	return nil
}
