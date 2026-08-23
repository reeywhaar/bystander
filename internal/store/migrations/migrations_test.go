package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"testing"
)

// released is every migration that has ever shipped, by name and by the sha256 of the file
// that declares it.
//
// This is what turns "never edit a released migration" from a comment into something the
// build enforces. Every deployment past a given migration has already recorded it as
// applied and will skip it forever, so editing one silently leaves the schema in front of
// the code different from the schema in the file — on those databases and no others.
//
// Adding a migration means adding a line here. Changing a hash that is already here means
// you have edited history; add a new migration instead.
//
// The hash is over the whole file, so a comment is as protected as the SQL. That is
// deliberate: the comment above a migration is usually the only record of why it exists,
// and quietly rewriting it is its own kind of losing the history.
var released = []struct{ name, sha string }{
	{"20260823030000_derived_initial_schema", "ec50025a77f339cb53322c5cf065eafb72fc862091f308380fc011c70912ba7f"},
	{"20260823030000_main_initial_schema", "017213ba33a8a7bf1e649664b79c54da634172c52dfc04235ecc599bb9454489"},
	{"20260823051000_derived_read_articles", "dc8052af5e712bccfcaf8ebc97c26de5764e560090bea6092e30227ca4eb3674"},
	{"20260823061500_main_article_window", "c17ad1b36a3230a6767e9bc0e196d1c17f377e81c7ec2fe8c89ac88cf30a0f5f"},
}

func all() []Migration {
	return append(append([]Migration{}, Main...), Derived...)
}

func hashOf(t *testing.T, name string) string {
	t.Helper()
	// Read from the package directory rather than an embed: this is a rule about the
	// source, and there is no reason to carry the source into the binary to enforce it.
	source, err := os.ReadFile(name + ".go")
	if err != nil {
		t.Fatalf("read %s.go: %v", name, err)
	}
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func TestMigrationsAreAppendOnly(t *testing.T) {
	declared := map[string]bool{}
	for _, m := range all() {
		declared[m.Name] = true
	}

	for _, want := range released {
		if !declared[want.name] {
			t.Errorf("%s has shipped and is no longer declared; a released migration cannot be removed",
				want.name)
			continue
		}
		if sha := hashOf(t, want.name); sha != want.sha {
			t.Errorf("%s has been edited since it shipped.\n"+
				"Databases that already applied it will never see the change. Add a new\n"+
				"migration instead. (was %s, now %s)", want.name, short(want.sha), short(sha))
		}
		delete(declared, want.name)
	}

	for name := range declared {
		t.Logf("%s is new; add it to `released` above:\n\t\t{%q, %q},", name, name, hashOf(t, name))
	}
	if len(declared) > 0 {
		t.Errorf("%d migrations have not been recorded; add them above", len(declared))
	}
}

// A migration's file is what orders it and what an error names, so both have to hold.
func TestEveryMigrationHasItsOwnFile(t *testing.T) {
	pattern := regexp.MustCompile(`^\d{14}_(main|derived)_[a-z0-9_]+$`)

	for _, m := range all() {
		if !pattern.MatchString(m.Name) {
			t.Errorf("%q is not <14-digit timestamp>_<main|derived>_<snake_case_name>", m.Name)
		}
		if _, err := os.Stat(m.Name + ".go"); err != nil {
			t.Errorf("%s is declared but %s.go does not exist; the name is what an error prints",
				m.Name, m.Name)
		}
		if m.Up == nil {
			t.Errorf("%s does nothing", m.Name)
		}
	}
}

// Two migrations sharing a timestamp have no defined order between them, which is a
// difference that would only ever show up on somebody else's database.
func TestTimestampsAreUnique(t *testing.T) {
	for _, list := range [][]Migration{Main, Derived} {
		seen := map[string]string{}
		for _, m := range list {
			stamp := strings.SplitN(m.Name, "_", 2)[0]
			if other, taken := seen[stamp]; taken {
				t.Errorf("%s and %s share a timestamp; their order is undefined", m.Name, other)
			}
			seen[stamp] = m.Name
		}
	}
}

// The name says which database it belongs to, and being in the wrong list would apply it to
// the wrong one.
func TestMigrationsAreInTheListTheyName(t *testing.T) {
	for _, m := range Main {
		if !strings.Contains(m.Name, "_main_") {
			t.Errorf("%s is in Main but is not named for it", m.Name)
		}
	}
	for _, m := range Derived {
		if !strings.Contains(m.Name, "_derived_") {
			t.Errorf("%s is in Derived but is not named for it", m.Name)
		}
	}
}
