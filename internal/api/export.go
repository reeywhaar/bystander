package api

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bystander/internal/app"
	"bystander/internal/store"
)

// ExportFormat names the shape of the document inside the archive, and ExportVersion is
// what anything reading it should check.
//
// A version rather than nothing, because the alternative to saying "1" now is guessing later
// whether a file predates a change. It costs six bytes.
const (
	ExportFormat  = "bystander-export"
	ExportVersion = 1
	// ExportEntry is the one file in the archive. A zip holding a single JSON document,
	// rather than the document itself, so that pictures or an OPML copy can join it later
	// without the thing people have already learned to open changing shape.
	ExportEntry = "export.json"
)

// exportArchive writes everything one account holds, as a zip.
//
// Streamed straight to the socket: `zip.NewWriter(w)` and the JSON encoded into the entry as
// it goes. No temporary file and no whole document in memory — a reader with a year of
// history has tens of thousands of read articles, and buffering that to disk to serve it
// would mean the export's peak cost was a function of how much somebody had read.
//
// The consequence is that the status is committed before most of the work happens, so the
// order below is deliberate: everything cheap and bounded is read *first*, while a refusal
// can still be an honest 500. What remains after the first byte is the two long sections,
// and if one of those fails the archive is abandoned without its central directory — see
// the end of this function.
func (s *Server) exportArchive(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	ctx := r.Context()

	account, err := s.store.ExportAccount(ctx, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tags, err := s.store.ExportTags(ctx, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	feeds, err := s.store.ExportFeeds(ctx, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pages, err := s.store.ExportPages(ctx, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Articles live in derived.db and feeds in main.db, and the two are never ATTACHed, so
	// an article's feed is named in Go rather than in SQL.
	names, err := s.store.FeedNames(ctx, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	feedIDs := make([]string, 0, len(names))
	for id := range names {
		feedIDs = append(feedIDs, id)
	}

	now := s.store.Now()
	filename := app.Name + "-" + safeName(account.Username) + "-" + now.Format("2006-01-02") + ".zip"

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	// The one endpoint that is not JSON, so it is the one endpoint that has to say so.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// The server's write timeout is a ceiling on one response, and this is the one response
	// that is not written in a burst — a reader with years of history on a slow connection
	// is exactly who would be cut off mid-archive, and a truncated zip is the failure this
	// endpoint takes the most trouble to avoid. The deadline is pushed forward on every
	// batch instead: a client still receiving keeps its connection for as long as it needs,
	// and one that has stopped is still cut off, which is what the timeout was for.
	extend := writeDeadline(w, s.store.Now)
	extend()

	archive := zip.NewWriter(w)
	entry, err := archive.Create(ExportEntry)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	doc := &jsonDoc{w: entry}
	doc.open()
	doc.member("format", ExportFormat)
	doc.member("version", ExportVersion)
	doc.member("exported_at", now.Unix())
	doc.member("instance", s.cfg.PublicURL.String())
	doc.member("account", account)
	doc.member("tags", tags)
	doc.member("feeds", feeds)
	doc.member("pages", pages)
	streamMember(doc, "read", s.pager(names, extend, func(after *store.ExportCursor) ([]store.ExportedArticle, error) {
		return s.store.ExportRead(ctx, p.ID, after, store.ExportBatch)
	}, func(a store.ExportedArticle) int64 { return a.ReadAt }))

	streamMember(doc, "unread", s.pager(names, extend, func(after *store.ExportCursor) ([]store.ExportedArticle, error) {
		return s.store.ExportUnread(ctx, p.ID, feedIDs, after, store.ExportBatch)
	}, func(a store.ExportedArticle) int64 { return a.PublishedAt }))

	doc.close()

	if doc.err != nil {
		// Deliberately not closing the archive. `zip.Writer.Close` is what writes the
		// central directory, and without one every tool that opens a zip refuses it —
		// which is the only honest way to fail after a 200 has gone out. The alternative
		// is a short archive that opens cleanly and is quietly missing half of somebody's
		// history, and an export nobody can tell is incomplete is worse than none.
		s.log.Error("an export was abandoned part-written",
			"principal", p.ID, "err", doc.err)
		return
	}
	if err := archive.Close(); err != nil {
		s.log.Error("an export could not be finished", "principal", p.ID, "err", err)
	}
}

// pager turns a batched store query into something streamMember can pull from.
//
// It also stops when the cursor fails to advance. That should be impossible — the cursor's
// second half is a primary key — and the check is here anyway, because the failure mode it
// guards against is an archive that never ends.
func (s *Server) pager(
	names map[string][2]string,
	extend func(),
	fetch func(*store.ExportCursor) ([]store.ExportedArticle, error),
	sortKey func(store.ExportedArticle) int64,
) func() ([]store.ExportedArticle, error) {
	var cursor *store.ExportCursor
	done := false

	return func() ([]store.ExportedArticle, error) {
		if done {
			return nil, nil
		}
		extend()
		batch, err := fetch(cursor)
		if err != nil {
			return nil, err
		}
		if len(batch) < store.ExportBatch {
			done = true
		}
		if len(batch) > 0 {
			last := batch[len(batch)-1]
			next := last.After(sortKey(last))
			if cursor != nil && *next == *cursor {
				done = true
			}
			cursor = next
		}
		for i := range batch {
			if feed, ok := names[batch[i].FeedID]; ok {
				batch[i].FeedURL, batch[i].FeedTitle = feed[0], feed[1]
			}
		}
		return batch, nil
	}
}

// ExportWriteWindow is how long a stalled export is given before the connection is cut.
//
// Per batch rather than per response: it is a ceiling on silence, not on the download.
const ExportWriteWindow = 2 * time.Minute

// writeDeadline returns a function that pushes this response's write deadline forward.
//
// A no-op where the deadline cannot be set — `httptest.NewRecorder` has no connection to
// set one on, and neither does a middleware that has wrapped the writer without passing
// `Unwrap` through. Failing to extend a deadline is not a reason to fail an export.
func writeDeadline(w http.ResponseWriter, now func() time.Time) func() {
	control := http.NewResponseController(w)
	if err := control.SetWriteDeadline(now().Add(ExportWriteWindow)); err != nil {
		return func() {}
	}
	return func() {
		_ = control.SetWriteDeadline(now().Add(ExportWriteWindow))
	}
}

// safeName reduces a username to something a filename can hold.
//
// Usernames are not constrained to be filename-safe and this one crosses into a header a
// browser turns into a path. Anything outside letters, digits, dash and underscore becomes a
// dash rather than being dropped, so two different names cannot collapse into one.
func safeName(username string) string {
	name := strings.Map(func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			return c
		default:
			return '-'
		}
	}, username)
	if name == "" {
		return "account"
	}
	return name
}

// jsonDoc writes a JSON object one member at a time.
//
// So that a section can be read, encoded and released before the next is read. A
// `json.Encoder` over a struct holding every section would mean the whole export existed in
// memory at once, which is the thing this endpoint is written not to do.
//
// Indented, because the file is meant to be opened and read by whoever exported it — and it
// costs nothing, since the archive compresses the whitespace away.
type jsonDoc struct {
	w   io.Writer
	n   int
	err error
}

func (d *jsonDoc) write(s string) {
	if d.err != nil {
		return
	}
	_, d.err = io.WriteString(d.w, s)
}

// key opens a member, with the comma that separates it from the last one.
func (d *jsonDoc) key(name string) {
	if d.n > 0 {
		d.write(",\n")
	}
	d.n++
	d.write("  " + strconv.Quote(name) + ": ")
}

func (d *jsonDoc) open()  { d.write("{\n") }
func (d *jsonDoc) close() { d.write("\n}\n") }

// member writes a whole value. For the sections small enough to hold at once.
func (d *jsonDoc) member(name string, value any) {
	if d.err != nil || value == nil {
		return
	}
	raw, err := json.MarshalIndent(value, "  ", "  ")
	if err != nil {
		d.err = err
		return
	}
	d.key(name)
	d.write(string(raw))
}

// streamMember writes an array by pulling batches until there are none left.
//
// A free function rather than a method because Go has no generic methods, and the
// alternative — `[]any` — would mean every caller converting a typed slice element by
// element to hand it back to the encoder.
func streamMember[T any](d *jsonDoc, name string, next func() ([]T, error)) {
	if d.err != nil {
		return
	}
	d.key(name)
	d.write("[")

	written := 0
	for {
		batch, err := next()
		if err != nil {
			d.err = err
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, item := range batch {
			raw, err := json.MarshalIndent(item, "    ", "  ")
			if err != nil {
				d.err = err
				return
			}
			if written > 0 {
				d.write(",")
			}
			d.write("\n    " + string(raw))
			written++
		}
		if d.err != nil {
			return
		}
	}

	if written == 0 {
		d.write("]")
	} else {
		d.write("\n  ]")
	}
}
