package store

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Migrations are files, one per change, named `<timestamp>_<snake_case_name>.sql` and
// applied in filename order. The timestamp is what orders them, so two people adding a
// migration on the same day do not collide.
//
// One directory per database, because the two are separate files with separate versions
// and a migration belongs to exactly one of them.
//
//	migrations/main/20260823030000_initial_schema.sql
//	migrations/derived/20260823051000_read_articles.sql
//
// NEVER EDIT A RELEASED FILE. Every deployment past it has already recorded it as applied
// and will skip the edit forever, so the schema in front of the code silently stops
// matching the schema in the file — on those databases and no others. Add a new one
// instead; migrate_test.go holds a hash of each and fails the build if one moves.
//
// The full argument for each table is in private/docs/entities.md.
//
//go:embed migrations
var migrationFS embed.FS

// A Migration is one file.
type Migration struct {
	// Name is the filename, which is what an error and the append-only guard say.
	Name string
	SQL  string
}

// mainMigrations owns what a person typed. This is the database worth backing up.
func mainMigrations() []Migration { return load("main") }

// derivedMigrations owns what the machine produced. Everything there is reconstructible
// from main.db plus one fetch cycle.
func derivedMigrations() []Migration { return load("derived") }

// load reads one database's migrations, in order.
//
// Panics rather than returning an error: the files are embedded, so a failure here is a
// build that should not have compiled rather than a condition to handle at runtime.
func load(database string) []Migration {
	dir := "migrations/" + database
	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		panic(fmt.Sprintf("read %s: %v", dir, err))
	}

	var out []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sql, err := fs.ReadFile(migrationFS, dir+"/"+entry.Name())
		if err != nil {
			panic(fmt.Sprintf("read %s/%s: %v", dir, entry.Name(), err))
		}
		out = append(out, Migration{Name: entry.Name(), SQL: string(sql)})
	}

	// fs.ReadDir already sorts, but the order here is the schema's history and is far too
	// important to inherit by accident.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
