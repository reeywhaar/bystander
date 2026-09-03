package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bystander/internal/config"
	"bystander/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// received is a stand-in for backio-agent: it keeps what was posted to it.
type received struct {
	mu     sync.Mutex
	bodies [][]byte
	names  []string
	// status is what it answers with. 200 unless a test says otherwise.
	status int
	reply  string
}

func (r *received) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.status != 0 && r.status != http.StatusOK {
			w.WriteHeader(r.status)
			_, _ = io.WriteString(w, r.reply)
			return
		}
		if req.Method != http.MethodPost {
			t.Errorf("the agent was sent %s, want POST", req.Method)
		}
		file, header, err := req.FormFile("backup")
		if err != nil {
			t.Errorf("no `backup` file in the request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		r.bodies = append(r.bodies, body)
		r.names = append(r.names, header.Filename)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *received) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *received) last(t *testing.T) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		t.Fatal("nothing was posted")
	}
	return r.bodies[len(r.bodies)-1]
}

// entries unpacks an archive into a map of name to contents.
func entries(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		t.Fatalf("the archive is not gzip: %v", err)
	}
	defer gz.Close()

	out := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read the tar: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		if hdr.Mode != 0o600 {
			t.Errorf("%s is mode %o, want 600 — the archive carries password hashes", hdr.Name, hdr.Mode)
		}
		out[hdr.Name] = data
	}
	return out
}

func pusher(t *testing.T, st *store.Store, url string, mode config.BackupMode) *Pusher {
	t.Helper()
	return &Pusher{
		Store: st,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		URL:   url,
		Mode:  mode,
		Every: time.Minute,
	}
}

/*
 * The one failure nobody discovers until the day they need it.
 *
 * Not merely that something was posted: that what came out is a SQLite database, extractable,
 * and one that opens. This used to be a curl in the publish workflow against a port that no
 * longer exists; here it can also open the result, which the shell could not.
 */
func TestABackupIsADatabaseThatOpens(t *testing.T) {
	st := testStore(t)
	agent := &received{}
	srv := agent.server(t)

	if err := pusher(t, st, srv.URL, config.BackupMain).Once(t.Context()); err != nil {
		t.Fatalf("Once(): %v", err)
	}

	files := entries(t, agent.last(t))
	main, ok := files[store.MainFile]
	if !ok {
		t.Fatalf("the archive holds %v, not %s", keys(files), store.MainFile)
	}
	if !strings.HasPrefix(string(main), "SQLite format 3") {
		t.Fatal("main.db does not carry SQLite's own magic")
	}

	// And it opens, with the schema on it — the half a `head -c 15` could never check.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, store.MainFile), main, 0o600); err != nil {
		t.Fatalf("write the restored database: %v", err)
	}
	if derived, ok := files[store.DerivedFile]; ok {
		if err := os.WriteFile(filepath.Join(dir, store.DerivedFile), derived, 0o600); err != nil {
			t.Fatalf("write the restored derived database: %v", err)
		}
	}
	restored, err := store.Open(dir)
	if err != nil {
		t.Fatalf("the restored database does not open: %v", err)
	}
	defer restored.Close()
	if _, err := restored.ListPrincipals(t.Context()); err != nil {
		t.Fatalf("the restored database has no schema on it: %v", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// What each mode carries, which is half of what a mode is.
func TestWhatEachModeCarries(t *testing.T) {
	for _, tc := range []struct {
		mode    config.BackupMode
		derived bool
	}{
		{config.BackupMain, false},
		{config.BackupRelaxed, true},
		{config.BackupAll, true},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			st := testStore(t)
			agent := &received{}
			srv := agent.server(t)

			if err := pusher(t, st, srv.URL, tc.mode).Once(t.Context()); err != nil {
				t.Fatalf("Once(): %v", err)
			}
			files := entries(t, agent.last(t))
			if _, ok := files[store.MainFile]; !ok {
				t.Errorf("%s did not carry %s", tc.mode, store.MainFile)
			}
			if _, ok := files[store.DerivedFile]; ok != tc.derived {
				t.Errorf("%s carries %s = %v, want %v", tc.mode, store.DerivedFile, ok, tc.derived)
			}
		})
	}
}

/*
 * The other half of what a mode is, and the whole point of pushing rather than being pulled
 * from: a copy is made because something worth copying changed.
 */
func TestEveryModeSendsWhenMainChangesAndNotBefore(t *testing.T) {
	for _, mode := range []config.BackupMode{config.BackupMain, config.BackupRelaxed, config.BackupAll} {
		t.Run(string(mode), func(t *testing.T) {
			st := testStore(t)
			agent := &received{}
			p := pusher(t, st, agent.server(t).URL, mode)

			// The first pass always sends: nothing has been kept anywhere yet.
			if err := p.Once(t.Context()); err != nil {
				t.Fatalf("first Once(): %v", err)
			}
			if agent.count() != 1 {
				t.Fatalf("%d archives after the first pass, want 1", agent.count())
			}

			for range 3 {
				if err := p.Once(t.Context()); err != nil {
					t.Fatalf("Once(): %v", err)
				}
			}
			if agent.count() != 1 {
				t.Errorf("%d archives, want 1 — nothing was written between them", agent.count())
			}

			// Something somebody typed.
			if _, err := st.CreatePrincipal(t.Context(), "alice", "correct-horse", store.RoleUser); err != nil {
				t.Fatalf("CreatePrincipal(): %v", err)
			}
			if err := p.Once(t.Context()); err != nil {
				t.Fatalf("Once() after a write: %v", err)
			}
			if agent.count() != 2 {
				t.Errorf("%d archives, want 2 — main.db changed", agent.count())
			}
		})
	}
}

/*
 * "all" is relaxed with a floor under it, and the floor exists for the one thing main.db never
 * sees: reading. An article marked read writes to derived.db and nothing else, so an instance
 * where somebody read all afternoon and changed no setting is, from the change check's point
 * of view, an idle one.
 */
func TestAllSendsOnItsFloorEvenWhenNothingChanged(t *testing.T) {
	st := testStore(t)
	agent := &received{}
	p := pusher(t, st, agent.server(t).URL, config.BackupAll)

	if err := p.Once(t.Context()); err != nil {
		t.Fatalf("first Once(): %v", err)
	}
	// Nothing has changed and the floor is nowhere near, so nothing more goes out.
	for range 3 {
		if err := p.Once(t.Context()); err != nil {
			t.Fatalf("Once(): %v", err)
		}
	}
	if agent.count() != 1 {
		t.Fatalf("%d archives, want 1 — the floor has not come round", agent.count())
	}

	// Wind the last copy back past the floor, which is what waiting would do.
	past := time.Now().Add(-config.BackupAllPeriod - time.Minute)
	digest, _, err := st.LastBackup(t.Context())
	if err != nil {
		t.Fatalf("LastBackup(): %v", err)
	}
	if err := st.RecordBackup(t.Context(), digest, past); err != nil {
		t.Fatalf("RecordBackup(): %v", err)
	}

	if err := p.Once(t.Context()); err != nil {
		t.Fatalf("Once() past the floor: %v", err)
	}
	if agent.count() != 2 {
		t.Errorf("%d archives, want 2 — the floor came round", agent.count())
	}

	// And it starts again from there: the push it just made is the new floor.
	if err := p.Once(t.Context()); err != nil {
		t.Fatalf("Once(): %v", err)
	}
	if agent.count() != 2 {
		t.Errorf("%d archives, want 2 — the floor was reset by the copy it caused", agent.count())
	}
}

// The other two have no floor: an instance nobody touches is never copied twice.
func TestOnlyAllHasAFloor(t *testing.T) {
	for _, mode := range []config.BackupMode{config.BackupMain, config.BackupRelaxed} {
		t.Run(string(mode), func(t *testing.T) {
			st := testStore(t)
			agent := &received{}
			p := pusher(t, st, agent.server(t).URL, mode)

			if err := p.Once(t.Context()); err != nil {
				t.Fatalf("first Once(): %v", err)
			}
			digest, _, err := st.LastBackup(t.Context())
			if err != nil {
				t.Fatalf("LastBackup(): %v", err)
			}
			// Long past any floor there could be.
			if err := st.RecordBackup(t.Context(), digest, time.Now().Add(-72*time.Hour)); err != nil {
				t.Fatalf("RecordBackup(): %v", err)
			}
			if err := p.Once(t.Context()); err != nil {
				t.Fatalf("Once(): %v", err)
			}
			if agent.count() != 1 {
				t.Errorf("%d archives, want 1 — %s waits for a change and nothing else", agent.count(), mode)
			}
		})
	}
}

/*
 * Recorded only once the agent has it.
 *
 * Recorded before, a rejected upload would leave this program believing a copy exists that
 * does not — and in the change-driven modes the next write to main.db would be the only thing
 * that ever made it try again.
 */
func TestARejectedUploadIsNotRememberedAsOne(t *testing.T) {
	st := testStore(t)
	agent := &received{status: http.StatusInsufficientStorage, reply: "the remote is full"}
	p := pusher(t, st, agent.server(t).URL, config.BackupMain)

	err := p.Once(t.Context())
	if err == nil {
		t.Fatal("Once() succeeded against an agent that refused")
	}
	// The agent's own words, not merely that it said no.
	if !strings.Contains(err.Error(), "the remote is full") {
		t.Errorf("the refusal does not carry what the agent said: %v", err)
	}
	if digest, _, err := st.LastBackup(t.Context()); err != nil {
		t.Fatalf("LastBackup(): %v", err)
	} else if digest != nil {
		t.Error("a refused upload was recorded as a copy that exists")
	}

	// And the next pass tries again rather than waiting for a write.
	agent.mu.Lock()
	agent.status = http.StatusOK
	agent.mu.Unlock()
	if err := p.Once(t.Context()); err != nil {
		t.Fatalf("Once() after the agent recovered: %v", err)
	}
	if agent.count() != 1 {
		t.Errorf("%d archives, want 1", agent.count())
	}
}

// Timestamped, so a store that keeps a series can order them.
func TestTheArchiveIsNamedForWhenItWasTaken(t *testing.T) {
	at := time.Date(2026, 9, 3, 4, 5, 6, 0, time.UTC)
	if got, want := Filename(at), "bystander-20260903_040506.tgz"; got != want {
		t.Errorf("Filename() = %q, want %q", got, want)
	}
}

// Nothing is sent, and nothing is attempted, unless an address was named.
func TestNoAddressMeansNoBackups(t *testing.T) {
	p := pusher(t, testStore(t), "", config.BackupMain)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Returns rather than blocking on a timer, which is what makes it safe to always start.
	p.Run(ctx)
}
