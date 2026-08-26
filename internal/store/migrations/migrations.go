// Package migrations holds the schema's history, one file per change.
//
// Files are named `<timestamp>_<database>_<snake_case_name>.go` and each declares a single
// Migration. The timestamp orders them and stops two people who add a migration on the same
// day from colliding; the database name says which of the two it belongs to.
//
// # Why Go rather than .sql files
//
// A schema change is usually SQL and nothing else. The ones that are not are the reason
// this is Go: moving data from one of the two databases to the other cannot be expressed in
// a file that is handed to one connection. A Migration gets its own transaction *and* a
// handle on the other database, so a change that has to carry rows across can.
//
// # NEVER EDIT A RELEASED MIGRATION
//
// Every deployment past it has already recorded it as applied and will skip the edit
// forever, so the schema in front of the code silently stops matching the schema in the
// file — on those databases and no others, which is the worst kind of difference to go
// looking for. Add a new migration instead. migrations_test.go holds a hash of every file
// and fails the build if one moves.
package migrations

import (
	"context"
	"database/sql"
)

// Context is what a migration is given.
type Context struct {
	Ctx context.Context

	// Tx is the database this migration belongs to, inside the transaction that also
	// stamps the new version. Everything a migration does here is atomic with being
	// recorded as done.
	Tx *sql.Tx

	// Other is the *other* database, for the migrations that have to move data across.
	//
	// Nothing done through it is atomic with Tx, and it cannot be: SQLite's multi-database
	// commit does not hold under WAL, which is why these are two connections and never
	// ATTACHed. So read from Other and write into Tx — that way a failure leaves the
	// source intact and the destination rolled back, which is the recoverable direction.
	Other *sql.DB
}

// Migration is one change to one database.
type Migration struct {
	// Name is the filename without its extension. It is what an error says, because
	// "20260823061500_main_article_window failed" is a file somebody can open and
	// "migration 2" is a thing they have to go and count.
	Name string

	// Up applies the change. There is no Down: this program does not support downgrading,
	// and a rollback nobody has ever run is a rollback that does not work.
	Up func(Context) error
}

// Main owns what a person typed: accounts, sessions, invitations, feeds, tags,
// subscriptions, settings. The database worth backing up.
//
// Order here is the schema's history. The runner sorts by name regardless, so a file added
// out of order still applies in the right place — but keep the list in order anyway, since
// it is the first thing anybody reads to find out what happened.
var Main = []Migration{
	mainInitialSchema,
	mainArticleWindow,
	mainArticleWindowPerFeed,
	mainRecoveryEmail,
	mainSMTPRelay,
	mainProvedRecoveryEmail,
	mainShares,
	mainJobs,
	mainPages,
	mainFrontPageName,
	mainFeedErrorBody,
	mainFeedFetchInterval,
	mainJobLabel,
	mainPageFilterLists,
	mainPrincipalSlug,
	mainPublicPages,
	mainInviteEmail,
	mainInviteSurvivesItsAccount,
	mainLandingPage,
	mainSessionDevices,
}

// Derived owns what the machine produced. Everything here is reconstructible from main.db
// plus one fetch cycle.
var Derived = []Migration{
	derivedInitialSchema,
	derivedReadArticles,
	derivedItemLinkIndex,
	derivedWideSlot,
	derivedImageSize,
	derivedImageProbed,
	derivedEditionPages,
	derivedShownPerPage,
	derivedReadArticlesKept,
	derivedImageRetryAt,
	derivedReadIsNotTheEditions,
}

// exec is the shape almost every migration takes: some SQL, in its own transaction.
func exec(sql string) func(Context) error {
	return func(m Context) error {
		_, err := m.Tx.ExecContext(m.Ctx, sql)
		return err
	}
}
