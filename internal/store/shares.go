package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// ShareTTL is how long a shared link works for.
//
// A week, because sharing is a conversation: somebody sends a link and the other person
// gets to it that evening, or at the weekend. Longer would leave a list of what somebody
// reads sitting at a guessable-length URL for months after they had forgotten making it.
const ShareTTL = 7 * 24 * time.Hour

// shareTokenBytes is 32 bytes — the same as an invitation's, and for the same reason: the
// URL is the whole of the authorisation, so it has to be past guessing.
const shareTokenBytes = 32

// MaxSharedFeeds bounds one link. Somebody sharing everything they read is the point; a
// list long enough to be a denial of service is not.
const MaxSharedFeeds = 500

// Share is a list of feeds somebody handed over, as stored.
type Share struct {
	PrincipalID string
	// OPML is the snapshot. What was shared keeps meaning what it meant when it was
	// shared — see the migration.
	OPML      string
	FeedCount int
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Expired reports whether this link has stopped working.
func (s *Share) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// CreateShare stores a list and returns the token that reaches it.
//
// The token is returned exactly once, here. What is stored is its hash, so a link that gets
// lost is made again rather than recovered.
func (s *Store) CreateShare(ctx context.Context, principalID, document string, feeds int) (*Share, string, error) {
	if feeds == 0 {
		return nil, "", Invalid("there is nothing to share")
	}
	if feeds > MaxSharedFeeds {
		return nil, "", Invalid("that is more feeds than one link can carry")
	}

	buf := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", fmt.Errorf("read random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	now := s.Now()
	share := &Share{
		PrincipalID: principalID,
		OPML:        document,
		FeedCount:   feeds,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ShareTTL),
	}
	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO shares (token_hash, principal_id, opml, feed_count, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), principalID, document, feeds,
		unix(share.CreatedAt), unix(share.ExpiresAt)); err != nil {
		return nil, "", fmt.Errorf("create share: %w", err)
	}
	return share, token, nil
}

// ShareByToken is the list a link reaches, or NotFound.
//
// Expiry is a NotFound rather than its own error. A link that has run out and a link that
// never existed are the same thing to whoever is holding it — there is nothing to do about
// either except ask for a new one — and telling them apart would say whether a given URL was
// ever real to anybody who tried enough of them.
func (s *Store) ShareByToken(ctx context.Context, token string) (*Share, error) {
	var (
		share   Share
		created int64
		expires int64
	)
	err := s.main.QueryRowContext(ctx, `
		SELECT principal_id, opml, feed_count, created_at, expires_at
		  FROM shares WHERE token_hash = ?`, hashToken(token)).
		Scan(&share.PrincipalID, &share.OPML, &share.FeedCount, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("that link is not one this instance knows")
	}
	if err != nil {
		return nil, err
	}
	share.CreatedAt = time.Unix(created, 0).UTC()
	share.ExpiresAt = time.Unix(expires, 0).UTC()

	if share.Expired(s.Now()) {
		return nil, NotFound("that link has expired")
	}
	return &share, nil
}

// PruneShares removes links that have run out. Reports how many went.
func (s *Store) PruneShares(ctx context.Context) (int64, error) {
	res, err := s.main.ExecContext(ctx,
		`DELETE FROM shares WHERE expires_at <= ?`, unix(s.Now()))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
