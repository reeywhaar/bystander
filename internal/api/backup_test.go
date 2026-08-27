package api

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// backupServer runs the backup listener, which is a different listener from the reader's and
// has to be started separately here for the same reason it is separate in production.
func (h *harness) backupServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h.api.BackupHandler())
	t.Cleanup(srv.Close)
	return srv
}

// members reads a tgz into a map of name to bytes.
func members(t *testing.T, body io.Reader) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(body)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	out := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if hdr.Mode != 0o600 {
			t.Errorf("%s is mode %o, want 0600 — the archive carries every password hash here",
				hdr.Name, hdr.Mode)
		}
		if hdr.Typeflag != tar.TypeReg {
			t.Errorf("%s is type %q, want a regular file: a directory entry would re-chmod "+
				"a data directory that already exists", hdr.Name, hdr.Typeflag)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = data
	}
	return out
}

// The archive is a database, not a copy of some bytes that were on disk.
//
// This is the whole reason the backup is an endpoint rather than a `tar` over the mounted
// volume. A SQLite database in WAL mode is three files and the committed state is spread
// across them, so a file-level copy of a running instance restores a database as it never was.
// The test that matters is therefore not "the archive contains main.db" — it is that what
// comes out opens, and has in it what the running instance had.
func TestABackupIsADatabaseThatOpens(t *testing.T) {
	h := newHarness(t)
	feed := newFeedServer(t, 6)

	h.signIn(store.RoleUser, "alice")
	h.expect(h.do(http.MethodPost, "/api/feeds", map[string]string{"url": feed.URL}), http.StatusCreated, nil)
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, nil)

	res, err := http.Get(h.backupServer(t).URL + BackupPath)
	if err != nil {
		t.Fatalf("GET %s: %v", BackupPath, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", BackupPath, res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "application/gzip" {
		t.Errorf("Content-Type = %q", got)
	}
	// An accurate length, so a truncated download is detectable by whatever fetched it
	// rather than at restore time, which is the worst possible moment to find out.
	if res.ContentLength <= 0 {
		t.Errorf("Content-Length = %d, want the archive's real size", res.ContentLength)
	}
	if got := res.Header.Get("Content-Disposition"); got == "" {
		t.Error("no Content-Disposition, so a fetch has no name to save it under")
	}

	files := members(t, res.Body)
	if _, ok := files[store.MainFile]; !ok {
		t.Fatalf("the archive has no %s; it holds %v", store.MainFile, keys(files))
	}

	// Open it. This is the assertion — everything above is about the envelope.
	restored := filepath.Join(t.TempDir(), store.MainFile)
	if err := os.WriteFile(restored, files[store.MainFile], 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+restored+"?mode=ro")
	if err != nil {
		t.Fatalf("open the restored database: %v", err)
	}
	defer db.Close()

	var username string
	if err := db.QueryRow(`SELECT username FROM principals`).Scan(&username); err != nil {
		t.Fatalf("read the restored database: %v", err)
	}
	if username != "alice" {
		t.Errorf("the restored database has %q, want alice", username)
	}
	var feeds int
	if err := db.QueryRow(`SELECT count(*) FROM feeds`).Scan(&feeds); err != nil {
		t.Fatal(err)
	}
	if feeds != 1 {
		t.Errorf("the restored database has %d feeds, want the one that was subscribed to", feeds)
	}
}

// derived.db is in the archive only when the operator asked for it.
//
// Not a detail: main.db is what somebody typed and cannot be recovered from anywhere, and
// derived.db is what the machine fetched and mostly can. Mostly — read_articles lives there, so
// leaving it out means a restored instance offers back every article its owner has read. The
// answer is the operator's, and the default is the smaller archive.
func TestTheArchiveCarriesDerivedOnlyWhenAsked(t *testing.T) {
	h := newHarness(t)

	res, err := http.Get(h.backupServer(t).URL + BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if files := members(t, res.Body); len(files) != 1 || files[store.MainFile] == nil {
		t.Errorf("by default the archive holds %v, want only %s", keys(files), store.MainFile)
	}

	h.api.cfg.BackupDerived = true

	res2, err := http.Get(h.backupServer(t).URL + BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	files := members(t, res2.Body)
	if files[store.MainFile] == nil || files[store.DerivedFile] == nil {
		t.Errorf("asked for derived, the archive holds %v, want both databases", keys(files))
	}
}

// The reader's routes are not on this listener, and this route is not on the reader's.
//
// That separation is the security model, because there is no other one: no session, no token.
// A route that leaked onto the reader's port would be every password hash on the instance,
// served to whoever a reverse proxy lets through.
func TestTheBackupListenerServesNothingElse(t *testing.T) {
	h := newHarness(t)
	backup := h.backupServer(t)

	for _, path := range []string{"/api/me", "/api/feeds", "/healthz", "/", "/login"} {
		res, err := http.Get(backup.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("the backup listener answered %s with %d, want 404", path, res.StatusCode)
		}
	}

	// And the other way round, which is the direction that would actually hurt.
	//
	// Not asserted on the status: the reader's catch-all hands any unrouted path to the SPA,
	// so /backup there is a 200 of HTML — which is fine, and is not what this is about. What
	// must never happen is an *archive* coming back, so that is what is checked.
	res := h.doAs(h.stranger(), http.MethodGet, BackupPath, nil)
	defer res.Body.Close()
	if got := res.Header.Get("Content-Type"); strings.HasPrefix(got, "application/gzip") {
		t.Fatalf("%s served an archive on the reader's listener", BackupPath)
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		t.Fatalf("%s served gzip on the reader's listener whatever it called it", BackupPath)
	}
}

// A timestamped name, so a backup store can keep a series rather than one file overwritten.
func TestTheArchiveIsNamedForWhenItWasTaken(t *testing.T) {
	at := time.Date(2026, 8, 25, 3, 15, 0, 0, time.UTC)
	if got, want := BackupFilename(at), "bystander-20260825_031500.tgz"; got != want {
		t.Errorf("BackupFilename() = %q, want %q", got, want)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
