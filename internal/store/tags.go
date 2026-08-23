package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"bystander/internal/ids"
)

// DefaultPriority is where an untouched feed or tag sits, so every adjustment reads as a
// move up or down from ordinary rather than away from an edge.
const DefaultPriority = 50

// MaxTagNameLen keeps a name to something that fits in a column of the manage page.
const MaxTagNameLen = 48

// Tag is one bucket in somebody's taxonomy.
//
// ParentID groups tags in the interface and takes no part in selecting a page — see
// private/docs/edition.md for what the hierarchical alternative would cost.
type Tag struct {
	ID          string
	PrincipalID string
	Name        string
	ParentID    string // empty at the root
	Priority    int
	CreatedAt   time.Time
}

// CreateTag adds a tag.
func (s *Store) CreateTag(ctx context.Context, principalID, name, parentID string, priority int) (*Tag, error) {
	name = strings.TrimSpace(name)
	if err := validateTagName(name); err != nil {
		return nil, err
	}
	if err := validatePriority(priority); err != nil {
		return nil, err
	}
	if parentID != "" {
		if _, err := s.TagByID(ctx, principalID, parentID); err != nil {
			return nil, Invalid("no tag %s to nest under", parentID)
		}
	}

	tag := &Tag{
		ID:          ids.New(ids.Tag),
		PrincipalID: principalID,
		Name:        name,
		ParentID:    parentID,
		Priority:    priority,
		CreatedAt:   s.Now(),
	}
	if _, err := s.main.ExecContext(ctx,
		`INSERT INTO tags (id, principal_id, name, parent_id, priority, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		tag.ID, principalID, name, nullString(parentID), priority, unix(tag.CreatedAt)); err != nil {
		if isUnique(err) {
			return nil, Conflict("you already have a tag called %q there", name)
		}
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return tag, nil
}

const tagColumns = `id, principal_id, name, parent_id, priority, created_at`

func scanTag(row interface{ Scan(...any) error }) (*Tag, error) {
	var (
		tag     Tag
		parent  sql.NullString
		created int64
	)
	if err := row.Scan(&tag.ID, &tag.PrincipalID, &tag.Name, &parent, &tag.Priority, &created); err != nil {
		return nil, err
	}
	tag.ParentID = parent.String
	tag.CreatedAt = time.Unix(created, 0).UTC()
	return &tag, nil
}

// TagByID returns one of this principal's tags.
//
// Scoped by principal in the query rather than checked afterwards. A tag id is opaque and
// unguessable, but "unguessable" is not an authorisation model, and the scoping costs a
// clause.
func (s *Store) TagByID(ctx context.Context, principalID, id string) (*Tag, error) {
	tag, err := scanTag(s.main.QueryRowContext(ctx,
		`SELECT `+tagColumns+` FROM tags WHERE id = ? AND principal_id = ?`, id, principalID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no tag %s", id)
	}
	return tag, err
}

// ListTags returns this principal's tags, ordered for display: by name, roots first.
func (s *Store) ListTags(ctx context.Context, principalID string) ([]*Tag, error) {
	rows, err := s.main.QueryContext(ctx,
		`SELECT `+tagColumns+` FROM tags WHERE principal_id = ? ORDER BY ifnull(parent_id,''), name`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Tag
	for rows.Next() {
		tag, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// UpdateTag changes what was passed and leaves the rest alone.
func (s *Store) UpdateTag(ctx context.Context, principalID, id string, name *string, parentID *string, priority *int) error {
	tag, err := s.TagByID(ctx, principalID, id)
	if err != nil {
		return err
	}

	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if err := validateTagName(trimmed); err != nil {
			return err
		}
		tag.Name = trimmed
	}
	if priority != nil {
		if err := validatePriority(*priority); err != nil {
			return err
		}
		tag.Priority = *priority
	}
	if parentID != nil {
		if err := s.checkTagParent(ctx, principalID, id, *parentID); err != nil {
			return err
		}
		tag.ParentID = *parentID
	}

	res, err := s.main.ExecContext(ctx,
		`UPDATE tags SET name = ?, parent_id = ?, priority = ? WHERE id = ? AND principal_id = ?`,
		tag.Name, nullString(tag.ParentID), tag.Priority, id, principalID)
	if err != nil {
		if isUnique(err) {
			return Conflict("you already have a tag called %q there", tag.Name)
		}
		return err
	}
	return expectOne(res, NotFound("no tag %s", id))
}

// checkTagParent refuses a parent that does not exist, or one that would close a cycle.
//
// Walking to the root rather than checking the immediate parent: A under B under C, and
// then C moved under A, is a cycle no single-step check would catch, and a cycle here
// makes the manage page recurse until it runs out of stack.
func (s *Store) checkTagParent(ctx context.Context, principalID, id, parentID string) error {
	if parentID == "" {
		return nil
	}
	if parentID == id {
		return Invalid("a tag cannot be its own parent")
	}

	seen := map[string]bool{id: true}
	for at := parentID; at != ""; {
		if seen[at] {
			return Invalid("that would put a tag inside itself")
		}
		seen[at] = true

		tag, err := s.TagByID(ctx, principalID, at)
		if err != nil {
			return Invalid("no tag %s to nest under", parentID)
		}
		at = tag.ParentID
	}
	return nil
}

// DeleteTag removes a tag. Its children are promoted to roots by the schema, because
// deleting "News" should not silently delete everything filed under it, and the
// subscriptions that carried it simply lose it.
func (s *Store) DeleteTag(ctx context.Context, principalID, id string) error {
	res, err := s.main.ExecContext(ctx, `DELETE FROM tags WHERE id = ? AND principal_id = ?`, id, principalID)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no tag %s", id))
}

func validateTagName(name string) error {
	if name == "" {
		return Invalid("a tag needs a name")
	}
	if len([]rune(name)) > MaxTagNameLen {
		return Invalid("a tag name is at most %d characters", MaxTagNameLen)
	}
	return nil
}

func validatePriority(p int) error {
	if p < 0 || p > 100 {
		return Invalid("a priority is between 0 and 100")
	}
	return nil
}

// nullString writes an empty string as SQL NULL, for the columns where absence is the
// point: a tag with no parent, an invitation with no creator.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
