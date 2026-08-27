package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// BackupPath is the only route the backup listener serves.
const BackupPath = "/backup"

// BackupHandler serves the backup listener.
//
// A listener of its own, and nothing else is on it: the reader's routes are unreachable here
// and this route is unreachable there. That separation is the security model, because there is
// no other one — no session, no token, no CSRF check, none of what defends the reader's port.
// Those defend a listener a reverse proxy exposes to the internet. This one is meant for a
// sibling container on a private network, and it is off unless an operator names an address.
//
// The reason it cannot simply live behind an admin session is the thing that would use it: a
// backup sidecar has no browser, no login and nobody to type a password every hour. Giving it a
// token would mean minting, storing and rotating a credential whose whole purpose is to be read
// by a container on the same private network — a lock on a door inside the house.
func (s *Server) BackupHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.HandleFunc("GET "+BackupPath, s.backup)
	// No csrfGuard, unlike [Server.Handler]. Those three checks defend a cookie-authenticated
	// listener against a browser being made to act on somebody's behalf; nothing here reads a
	// cookie, and the client is a container with no browser in it.
	return mux
}

// BackupFilename is the archive's name, timestamped so a backup store can keep a series.
func BackupFilename(t time.Time) string {
	return "bystander-" + t.UTC().Format("20060102_150405") + ".tgz"
}

// backup writes the databases as a gzipped tar.
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	// One reading, so the entry timestamps and the filename cannot straddle a second.
	now := time.Now()

	archive, err := s.buildArchive(r.Context(), now)
	if err != nil {
		s.log.Error("backup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "backup: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", strconv.Itoa(archive.Len()))
	w.Header().Set("Content-Disposition", `attachment; filename="`+BackupFilename(now)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(archive.Bytes())
}

// buildArchive renders the tgz in memory rather than streaming it.
//
// A tar header needs its entry's size up front, so the databases have to be in hand anyway —
// and a failure partway through a streamed 200 would look like a success while producing a
// truncated archive. A backup that fails loudly is worth a great deal more than one that fails
// quietly, since nobody finds out which they had until the day they need it.
func (s *Server) buildArchive(ctx context.Context, now time.Time) (*bytes.Buffer, error) {
	files, err := s.store.Snapshot(ctx, s.cfg.BackupDerived)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}

	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		// 0600 throughout: the archive carries every password hash and session on the
		// instance, so an extracted copy should not be readable by anyone else. No directory
		// entries, so extracting cannot re-chmod a data directory that already exists.
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     f.Name,
			Mode:     0o600,
			Size:     int64(len(f.Data)),
			ModTime:  now.UTC().Truncate(time.Second),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("write %s header: %w", f.Name, err)
		}
		if _, err := tw.Write(f.Data); err != nil {
			return nil, fmt.Errorf("write %s: %w", f.Name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}
	return &out, nil
}
