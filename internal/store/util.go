package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
)

// isUnique reports whether err is a violated unique constraint.
//
// A string match, because modernc's driver reports these as a plain error rather than
// something with a code to compare. It is checked against a message SQLite has emitted
// unchanged for many years, and the failure mode if that ever moves is a 500 where a 409
// belonged — a bad error page, not bad data.
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// expectOne turns "the UPDATE matched nothing" into notFound.
//
// Without this, updating a row that is not there succeeds silently, and the caller
// reports success for work it did not do.
func expectOne(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}

// hashToken is how every secret this program holds is stored: session ids, invitation
// tokens. The value itself is never written down, so a database file, a backup or a heap
// dump contains nothing replayable.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// classified is an error that carries a class for the API to map onto a status code, and a
// sentence for a person to read — without the class's own name in front of it.
//
// NotFound("no tag %s", id) reads as "not found: no tag t_1" wherever it
// is shown, and these sentences are shown: they are the text of the API's refusals and the
// text the interface renders. Unwrap is what keeps errors.Is working, so nothing that
// classifies an error had to change.
type classified struct {
	class error
	msg   string
}

func (c classified) Error() string { return c.msg }
func (c classified) Unwrap() error { return c.class }

// NotFound, Conflict and Invalid build a classified error. Exported because the packages
// above this one refuse things too, and they should refuse in the same vocabulary.
func NotFound(format string, a ...any) error {
	return classified{ErrNotFound, fmt.Sprintf(format, a...)}
}

func Conflict(format string, a ...any) error {
	return classified{ErrConflict, fmt.Sprintf(format, a...)}
}

func Invalid(format string, a ...any) error {
	return classified{ErrInvalid, fmt.Sprintf(format, a...)}
}

// expectSome turns "nothing matched" into a not-found, and lets any number above that through.
//
// The sibling of expectOne, for a statement that legitimately touches more than one row — an
// article marked read across every page it happens to be on, for instance.
func expectSome(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}

// inList turns a slice of ids into arguments and the matching placeholders for an IN clause.
func inList(ids []string) ([]any, string) {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args, strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
}
