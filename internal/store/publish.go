package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"bystander/internal/ids"
)

// InstanceSettings are the answers that belong to the instance rather than to anybody on it.
//
// Both off until an administrator says otherwise, and the asymmetry between them is the point:
// publishing is reversible and indexing is not. Taking a page down is a switch; taking it out
// of somebody else's search index is a request nobody controls.
type InstanceSettings struct {
	// PublicPages is whether anybody here may publish a page at all. Turning it off takes
	// every published page down rather than only stopping new ones — it is the instance's
	// answer, not a default for pages to inherit.
	PublicPages bool
	// PublicIndexing is a ceiling on the per-page choice, not a default for it: where this is
	// off, no page is indexable however it is set, and the control is not offered.
	PublicIndexing bool
}

// Instance reads the settings that belong to the instance.
//
// A missing row is the same answer as a row of noes, so a fresh database needs no seeding and
// an administrator who has never opened the screen has already made the safe choice.
func (s *Store) Instance(ctx context.Context) (InstanceSettings, error) {
	var out InstanceSettings
	err := s.main.QueryRowContext(ctx,
		`SELECT public_pages, public_indexing FROM instance_settings WHERE singleton = 1`).
		Scan(&out.PublicPages, &out.PublicIndexing)
	if errors.Is(err, sql.ErrNoRows) {
		return InstanceSettings{}, nil
	}
	return out, err
}

// SetInstance writes them.
func (s *Store) SetInstance(ctx context.Context, in InstanceSettings) error {
	_, err := s.main.ExecContext(ctx, `
		INSERT INTO instance_settings (id, singleton, public_pages, public_indexing, updated_at)
		VALUES (?, 1, ?, ?, ?)
		ON CONFLICT (singleton) DO UPDATE
		   SET public_pages    = excluded.public_pages,
		       public_indexing = excluded.public_indexing,
		       updated_at      = excluded.updated_at`,
		ids.New(ids.Instance), in.PublicPages, in.PublicIndexing, unix(s.Now()))
	return err
}

// PublishPage puts a page at an address, or moves it to a different one.
//
// The caller has already established that the instance permits it and that the owner has a
// public name; this is the write. Both of those are checked where the request arrives, because
// they produce different answers — "not on this instance" and "you need a name first" — and a
// single refusal here could say neither.
func (s *Store) PublishPage(ctx context.Context, pageID, slug string, indexable bool) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if err := checkSlug(slug); err != nil {
		return err
	}

	res, err := s.main.ExecContext(ctx,
		`UPDATE pages SET publish_slug = ?, published = 1, indexable = ? WHERE id = ?`,
		slug, indexable, pageID)
	if isUnique(err) {
		return Conflict("you have already published a page at %q", slug)
	}
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no page %s", pageID))
}

// UnpublishPage takes a page down, and remembers where it was.
func (s *Store) UnpublishPage(ctx context.Context, pageID string) error {
	res, err := s.main.ExecContext(ctx,
		`UPDATE pages SET published = 0 WHERE id = ?`, pageID)
	if err != nil {
		return err
	}
	return expectOne(res, NotFound("no page %s", pageID))
}

// UnpublishAll takes down everything one person has published, and says how many.
//
// For giving up a public name: the name is what the addresses are built from, so keeping the
// pages up without it would leave them reachable at an address nothing can produce. Taking them
// down is the honest reading of "I no longer want to be known here".
func (s *Store) UnpublishAll(ctx context.Context, principalID string) (int, error) {
	res, err := s.main.ExecContext(ctx,
		`UPDATE pages SET published = 0 WHERE principal_id = ? AND published = 1`, principalID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// PublishedPage resolves a public address to the page behind it, with its lists loaded.
//
// Not found covers every way this can fail — no such person, no such page, taken down, an
// account switched off, or an instance that does not serve pages to strangers at all. A
// stranger has no business learning which of those it was, and an owner already knows.
func (s *Store) PublishedPage(ctx context.Context, person, page string) (*Page, error) {
	settings, err := s.Instance(ctx)
	if err != nil {
		return nil, err
	}
	missing := NotFound("no page at /p/%s/%s", person, page)
	if !settings.PublicPages {
		return nil, missing
	}

	found, err := scanPage(s.main.QueryRowContext(ctx,
		`SELECT `+prefixed(pageColumns, "g")+`
		   FROM pages g
		   JOIN principals p ON p.id = g.principal_id
		  WHERE p.slug = ? AND p.slug <> '' AND p.disabled_at IS NULL
		    AND g.publish_slug = ? AND g.published = 1`,
		strings.ToLower(person), strings.ToLower(page)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, missing
	}
	if err != nil {
		return nil, err
	}

	// Indexing needs both answers and the instance's is the ceiling. Applied here rather than
	// at the call site, so that nothing serving this page can forget to ask.
	found.Indexable = found.Indexable && settings.PublicIndexing
	return found, loadPageLists(ctx, s.main, found)
}
