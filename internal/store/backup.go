package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// SnapshotFile is one database, whole, in memory.
type SnapshotFile struct {
	// Name is the file's name in the data directory — `main.db`, `derived.db` — so an
	// archive built from these extracts straight back over the mount.
	Name string
	Data []byte
}

// Snapshot copies the databases into memory, consistently, while they are being written.
//
// `VACUUM INTO`, which is the whole reason this exists as a method rather than as a `tar` over
// the mounted directory. A SQLite database in WAL mode is not one file: it is `main.db`,
// `main.db-wal` and `main.db-shm`, and the committed state is spread across them. Copy those
// three at slightly different moments — which is what any file-level backup of a running
// service does — and what you have restored is a database as it never was. `VACUUM INTO` asks
// SQLite for the database as of one read transaction, with the log folded in, and hands back a
// single self-contained file with no sidecars.
//
// It is also smaller than the original: the copy is written page by page with no free list, so
// a database that has had a year of articles pruned out of it comes back the size of what is
// actually in it. Measured on a real instance, 315KB became 262KB.
//
// derived is optional and off unless asked for, which is the two-database split showing up in
// the backup policy: main.db is what somebody typed and cannot be recovered from anywhere,
// derived.db is what the machine fetched and mostly can. Mostly — `read_articles` lives there,
// so an instance restored without it offers back every article its owner has already read. That
// is a judgement about how much archive to carry, which is why it is the operator's to make.
func (s *Store) Snapshot(ctx context.Context, derived bool) ([]SnapshotFile, error) {
	// A directory of this program's own rather than the data directory: VACUUM INTO refuses
	// to overwrite, so it needs somewhere nothing else is writing, and a failed run should
	// not leave a half-written database sitting beside the real ones where the next thing
	// along the line might mistake it for one.
	tmp, err := os.MkdirTemp("", "bystander-snapshot-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	// main.db first. It is the file that must survive, so it is the first thing out of the
	// archive and the first thing back into a data directory.
	sources := []struct {
		name string
		db   *sql.DB
	}{{MainFile, s.main}}
	if derived {
		sources = append(sources, struct {
			name string
			db   *sql.DB
		}{DerivedFile, s.derived})
	}

	out := make([]SnapshotFile, 0, len(sources))
	for _, source := range sources {
		path := filepath.Join(tmp, source.name)
		if _, err := source.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", source.name, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read the %s snapshot: %w", source.name, err)
		}
		out = append(out, SnapshotFile{Name: source.name, Data: data})
	}
	return out, nil
}
