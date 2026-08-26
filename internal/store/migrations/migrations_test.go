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
// In the order they shipped, which is the order their names sort in. Nothing reads it that
// way — the tests below treat it as a set — but it is a ledger of what has gone out, and a
// ledger out of order is one nobody can scan for the entry they are looking for.
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
	{"20260823103614_main_article_window_per_feed", "77725541a887d569f18b1142fa82b32e6af3550c118b407bfdd5e78305dcb88b"},
	{"20260823190043_main_recovery_email", "7e319816e63e89fe11991529e6b8b881365bfdc7542215f394e7b50c557c4e29"},
	{"20260823190456_main_smtp_relay", "db61f782a58927b09822f80133c3f8494539ae2086ce9a8900b91f85dd54e79f"},
	{"20260823195146_main_proved_recovery_email", "ed5ef7cc86cb7d380c17accb3bd5dd0d34afa0883c43d9ef1a6ac4c5f36db29c"},
	{"20260823201425_derived_item_link_index", "4acc0f208b17babb1ea7052bfab60897a5b0d193c87902922c4f50cef2464d57"},
	{"20260823203459_main_shares", "16d47cae350de5981d5b54324fd54e52fd82878fae7d9d4a18710d656882fe69"},
	{"20260823232919_derived_wide_slot", "4529e33544d2eed4609a104f5a7a27767e9d50276430d60aaffc33c1d1bba1fc"},
	{"20260824013412_derived_image_size", "2db9ce454aa832729f26acf5b44ef25c1ac295f8ac276c524210f9597a688bd5"},
	{"20260824013604_main_jobs", "0dd9e98cbf2388108b4a07c21362bdd60061928c87377f66cd2b7a118054c3ed"},
	{"20260824015002_derived_image_probed", "14c8b02f812b241a948324d45e963f5ad9c8ed1b45bac81ebeea8604e76e2ae0"},
	{"20260824030000_main_pages", "e9435b67b5e49146b72dfa1a5bd5e2b32091d568fd7c3f37edddccc55b7d42e5"},
	{"20260824030100_derived_edition_pages", "097e9969468400afa129a031cc1c49f64533d1836b77a1fe1ad7a277fe054d69"},
	{"20260824040000_derived_shown_per_page", "9a923a678a5a1a28cdf8e9d5c7f6d15cb8a860905b23261d0caf6f0b0575785d"},
	{"20260824050000_main_front_page_name", "f6f626d4d2bdefcfa6f85ed603ea19afcfc7f57f19d1739143da9a7e8f315c14"},
	{"20260824060000_derived_read_articles_kept", "57c4727464dff60bbe9336fe9aa9c6be161da55c6b0a09882f67a42c03dcbb42"},
	{"20260824070000_main_feed_error_body", "2d829bd7d4c763486d96d56f5b295d6f6c46d4d66c84f381d31f959f265bf64b"},
	{"20260824080000_main_feed_fetch_interval", "b3a1fe2219388ca49236fc4f7a0bb5d1c6bca3799665ce846ef5d8e3b7abd376"},
	{"20260825010000_main_job_label", "f64adf7f7d7dd8fcfe78172972ee1c53d7d73373dc1bc2a8529083530eab891a"},
	{"20260825020000_main_page_filter_lists", "03eb05b4a65a173dbf30a2bc8c60bb3d96964256a8d43006625d50fc941ef4cb"},
	{"20260825030000_derived_image_retry_at", "52620142eed1ae5c7e5e177ca263b807996f0089d56e8b48a80289dfab1e9106"},
	{"20260825040000_main_principal_slug", "1fbde99d0ef1f0e696a13c8eacdc9325962fd6d8cc0699d773b9c4585e09304b"},
	{"20260825062029_main_public_pages", "90122553cc925e840a238c6f6c7813b7c470bc54d563ec6bbbe5d9febfd546a5"},
	{"20260825064927_derived_read_is_not_the_editions", "c7124047c8ba466ad70a31395f2935546affca7eeb64cf89c188ea94de4f7ec7"},
	{"20260825224030_main_invite_email", "8d45fa9c9ad7537fbeeedf1d05c4087ebb34d0d9637448476f82126cdcdcb8e7"},
	{"20260825235815_main_invite_survives_its_account", "92cecd85e42a197867093fef2892301858aaaed162b9031d44733d5367ccce0f"},
	{"20260826005150_main_landing_page", "fdc55ffd942a36467ca51cb701af501de050b4d695d5af659887be76ca0b36d1"},
	{"20260826015027_main_session_devices", "9cabb87dc7371f01e4310aad98b5d8d4e826f77e6b54d540be2365ef6c324da3"},
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
