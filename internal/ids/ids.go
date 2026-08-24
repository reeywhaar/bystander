// Package ids generates the opaque, time-sortable identifiers every entity carries.
//
// An id is a prefix plus 26 characters of Crockford base32 over 16 bytes: a 6-byte
// big-endian millisecond timestamp followed by 10 random bytes. That layout is ULID's,
// and because the alphabet is in ASCII order and the length is fixed, the encoded strings
// sort chronologically — which makes `ORDER BY id` a time order and makes it possible to
// tell by eye which of two ids is older.
//
// The encoding is Go's standard base32 over those bytes rather than ULID's canonical
// 128-bit-integer encoding, so these are not interchangeable with other ULID
// implementations. They are internal opaque ids and are never parsed back, so that costs
// nothing and saves a dependency.
//
// [Derive] is the exception to all of that, and deliberately so: an article's identity is
// its publisher's, not ours, so its id is a digest of that identity rather than a fresh
// random one. Those do not sort by time. Nothing orders articles by id — they are ordered
// by publication, which is the order that means something about an article — but a new
// caller of Derive should check that the same is true of whatever it is naming.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"strings"
	"time"
)

// Prefixes make an id self-describing in a log line.
const (
	Principal    = "p_"
	Invite       = "i_"
	Tag          = "t_"
	Feed         = "f_"
	Subscription = "s_"
	Article      = "a_"
	Edition      = "e_"
	SMTP         = "m_"
	Job          = "j_"
)

// crockford omits I, L, O and U, so an id cannot be misread between similar glyphs or
// accidentally spell something.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var crockford = base32.NewEncoding(alphabet).WithPadding(base32.NoPadding)

// EncodedLen is the number of base32 characters after the prefix.
const EncodedLen = 26

// New returns a new id with the given prefix, timestamped now.
//
// It does not return an error. crypto/rand.Read is documented never to fail on any
// platform this runs on, and threading an error through every entity constructor for a
// condition that cannot occur costs more than it protects.
func New(prefix string) string { return newAt(prefix, time.Now()) }

func newAt(prefix string, t time.Time) string {
	var b [16]byte
	// 48 bits of milliseconds covers dates to the year 10889. The low 6 bytes of the
	// millisecond timestamp go in big-endian so byte order matches time order.
	ms := uint64(t.UTC().UnixMilli())
	binary.BigEndian.PutUint64(b[:8], ms<<16)
	// Overwrites the two bytes the shift left as zero padding, and fills the rest.
	rand.Read(b[6:])
	return prefix + crockford.EncodeToString(b[:])
}

// Derive returns the id for a thing that already has an identity of its own.
//
// The same parts always produce the same id, on any instance and after any number of
// rebuilds. That is the point: an article's identity belongs to its publisher — a feed id
// and a guid — and minting a fresh random id for it every fetch would mean the same article
// held a different id depending on when we happened to first see it. It does not, and
// anything keyed by that id survives the article being pruned and coming back.
//
// The trade is that these do not sort by time, because there is no timestamp in them. Only
// use Derive where something else orders the rows — articles are ordered by publication,
// which is the order that means anything about them anyway.
//
// Not a security boundary. It is a digest of values the publisher chose, so anybody holding
// the feed can compute it; ids are opaque handles, not secrets, and this one no more than
// the rest.
func Derive(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		// A separator that cannot appear in the parts, so ("ab", "c") and ("a", "bc")
		// cannot collide by concatenation.
		h.Write([]byte{0})
	}
	return prefix + crockford.EncodeToString(h.Sum(nil)[:16])
}

// Valid reports whether s looks like an id of the given kind. It checks shape only: an id
// is opaque, so this is for rejecting obvious nonsense before it reaches a query, not for
// authorising anything.
func Valid(prefix, s string) bool {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok || len(rest) != EncodedLen {
		return false
	}
	for _, r := range rest {
		if !strings.ContainsRune(alphabet, r) {
			return false
		}
	}
	return true
}
