package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"bystander/internal/session"
)

// Island prefixes. Which shell a navigation receives is decided here, in one table, and
// nowhere else.
const (
	LoginPath  = "/login"
	InvitePath = "/invite/"
	// ForgotPath asks for a way back into an account, and RecoverPath is where that way
	// leads. Both belong to the login island: whoever is on them has no session by
	// definition, and one of them is reached from a mail rather than from this site at all.
	ForgotPath  = "/forgot"
	RecoverPath = "/recover/"
	ManagePath  = "/manage"
	// A shared link lands in the manage island: what it opens is the feed picker, which
	// already lives there, and what somebody does next is subscribe to things.
	SharePath = "/share"
	AdminPath = "/admin"

	// PublicPath is where somebody's published pages live: /p/<their name>/<the page>.
	//
	// Its own island rather than the reader's, and the reason is what the reader is: an
	// application for somebody with an account, which knows about sessions and settings and
	// marking things read. A stranger opening a link should be handed a page, not the shell
	// of a product they have no account for and half of whose controls would refuse them.
	PublicPath = "/p/"
)

// SPA serves the built React bundle.
//
// Straight off the disk, a request at a time, and nothing of the bundle is held but an index.
// It used to read every file into memory at startup — the gzipped bytes and a decompressed copy
// of each besides — which was about two megabytes of heap on a program whose whole live heap is
// four, spent on bytes the OS page cache already keeps warm and can drop when it needs the room.
// That copy dates from when the bundle was embedded in the binary; see config.DefaultWebDir.
//
// The index is what startup still builds: which paths exist, where each lives, its content type
// and a validator. Computing the validator means reading each file once, streamed through the
// hash and not kept.
//
// # Why not http.FileServerFS
//
// Everything that made holding the bytes attractive is still true of it: the implicit
// /index.html redirect, directory listings, and range handling we do not want. None of that
// needed the bytes held, only the handler written by hand, so it still is. The content type
// comes from an explicit table rather than mime.TypeByExtension too, which on a minimal
// container depends on an /etc/mime.types that may not be installed.
//
// # The bundle is not expected to change under a running process
//
// It cannot in the image, where it is a layer. From a checkout it can — a rebuild while the
// server runs — and then what was indexed at startup no longer matches the files: a validator
// that answers 304 for a changed file, and new asset names this has never heard of. Restart
// after a rebuild, which is what was already needed to see one.
type SPA struct {
	dist   fs.FS
	assets map[string]asset

	// Four documents rather than one, because these are four applications with four
	// audiences. The login shell is the only one an unauthenticated visitor receives, and
	// the admin bundle is not merely hidden from a subscriber — it is never sent to them.
	// See web/vite.config.ts and docs/frontend.md.
	index  asset
	login  asset
	manage asset
	admin  asset
	public asset

	hasIndex bool
	log      *slog.Logger

	// landing answers whether "/" shows the landing page to somebody without a session.
	//
	// Injected rather than read here, because this type owns a directory of bytes and knows
	// nothing about a database — and the answer is an instance setting. Nil means yes, which
	// is what a test constructing an SPA on its own gets and what the setting defaults to.
	landing func(*http.Request) bool
}

// asset is where a file is and what to say about it — not the file.
//
// The image gzips the text files in the bundle at build time (see the Dockerfile), so most of
// these name a ".gz" and are sent as they are to a client that accepts gzip, which is every
// browser. The rest — curl, a health check, the smoke test in CI — get it decompressed as it is
// streamed. Pictures are stored plain, because gzipping a raster image makes it bigger.
type asset struct {
	// file is the path inside dist, ".gz" included when gzipped. Empty for the placeholder,
	// which is not a file.
	file string
	// inline is the placeholder's body, which is the one document with nothing on disk.
	inline []byte

	etag    string
	ctype   string
	gzipped bool

	// size is the file as stored; plain is what it decompresses to. The same number when the
	// file is not gzipped. Both are known up front so every response can say how long it is,
	// whichever form it takes.
	size, plain int64
}

// placeholderIndex stands in when web/dist holds no build.
//
// It lives here, in reviewable Go source, rather than as a committed stub index.html. A
// stub file would be overwritten by every local `npm run build` and show up forever as a
// modified file in git status; and diagnosing the missing build once at startup, where it
// can be logged, beats serving a blank page and letting somebody work out why.
const placeholderIndex = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>bystander</title>
<style>
 body{font:16px/1.6 system-ui,sans-serif;max-width:34rem;margin:4rem auto;padding:0 1rem;
      background:#12120f;color:#efece4}
 code{background:#1d1d19;border:1px solid #2e2e28;border-radius:6px;padding:.15em .4em}
</style></head>
<body>
<h1>The frontend has not been built</h1>
<p>This binary was compiled without a React bundle in <code>web/dist</code>, so there is
   no interface to serve. The API is running normally.</p>
<pre><code>cd web &amp;&amp; npm ci &amp;&amp; npm run build</code></pre>
<p>…and rebuild the binary, or use the published image
   <code>ghcr.io/reeywhaar/bystander</code>, which always contains one.</p>
</body></html>
`

// NewSPA indexes dist. A missing index.html is not fatal: the API is still useful and the
// placeholder explains itself.
func NewSPA(dist fs.FS, log *slog.Logger) (*SPA, error) {
	s := &SPA{dist: dist, assets: make(map[string]asset), log: log}

	err := fs.WalkDir(dist, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		a, err := describe(dist, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		// Registering "foo.js.gz" under "/foo.js" means nothing else — not this package,
		// not the bundle's own asset references — has to know the difference.
		name, _ := strings.CutSuffix(p, ".gz")
		s.assets["/"+name] = a
		return nil
	})
	if err != nil {
		return nil, err
	}

	if idx, ok := s.assets["/index.html"]; ok {
		s.index, s.hasIndex = idx, true
	} else {
		b := []byte(placeholderIndex)
		s.index = asset{inline: b, etag: etagOf(b), ctype: "text/html; charset=utf-8",
			size: int64(len(b)), plain: int64(len(b))}
		log.Warn("serving a placeholder page: web/dist/index.html is missing, so this build has no frontend")
	}

	// A missing island falls back to the reader's shell rather than 404ing. A link that
	// loads something is recoverable; one that dead-ends looks to its holder like the
	// link itself is wrong, and they will go and ask for another that behaves the same.
	for _, island := range []struct {
		file string
		into *asset
		name string
	}{
		{"/login.html", &s.login, "login"},
		{"/manage.html", &s.manage, "manage"},
		{"/admin.html", &s.admin, "admin"},
		{"/public.html", &s.public, "public"},
	} {
		if a, ok := s.assets[island.file]; ok {
			*island.into = a
			continue
		}
		*island.into = s.index
		if s.hasIndex {
			log.Warn("an island is missing from the bundle; it will load the reader's shell",
				"island", island.name, "file", island.file)
		}
	}
	return s, nil
}

// describe reads one file through once to learn what the index needs, and keeps none of it.
//
// The validator is taken over the *uncompressed* bytes, so both representations of a file share
// one. That is what makes Vary: Accept-Encoding correct rather than a way for a cache to hand
// somebody the wrong encoding — and it is why a gzipped file is decompressed here, into the hash
// and nowhere else.
func describe(dist fs.FS, p string) (asset, error) {
	f, err := dist.Open(p)
	if err != nil {
		return asset{}, err
	}
	defer f.Close()

	stored := &counter{r: f}
	var src io.Reader = stored
	name, gzipped := strings.CutSuffix(p, ".gz")
	if gzipped {
		z, err := gzip.NewReader(stored)
		if err != nil {
			return asset{}, err
		}
		defer z.Close()
		src = z
	}

	sum := sha256.New()
	plain, err := io.Copy(sum, src)
	if err != nil {
		return asset{}, err
	}
	// Whatever gzip left unread — a trailer it did not need — still counts toward what is
	// on disk, and the stored length is what a gzip response says it is.
	if _, err := io.Copy(io.Discard, stored); err != nil {
		return asset{}, err
	}
	return asset{
		file:    p,
		etag:    `"` + hex.EncodeToString(sum.Sum(nil)[:16]) + `"`,
		ctype:   contentType(name),
		gzipped: gzipped,
		size:    stored.n,
		plain:   plain,
	}, nil
}

// counter is a reader that remembers how much came through it.
type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)
	return n, err
}

func (s *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	clean := path.Clean(r.URL.Path)
	if a, ok := s.assets[clean]; ok && !strings.HasSuffix(clean, ".html") {
		// Vite content-hashes these names, so a given URL's bytes never change and the
		// browser can be told to keep them for a year.
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		s.serve(w, r, a)
		return
	}

	// Fall back to a shell only for things that look like navigation. A missing /app.js
	// should be a 404 in devtools, not an HTML document served with a JavaScript content
	// type — that failure presents as a MIME-type error with no hint that the file simply
	// is not there.
	if !looksLikeNavigation(r, clean) {
		writeError(w, http.StatusNotFound, "not found: "+r.URL.Path)
		return
	}

	// no-cache, never no-store: a shell is tiny, a 304 is the common case, and caching it
	// hard would strand a browser on a stale bundle reference after a deploy.
	w.Header().Set("Cache-Control", "no-cache")
	// "/" answers with a different document depending on whether a session cookie came with
	// the request, so a shared cache that stored one visitor's copy would hand a stranger
	// somebody's front page shell — or, more likely, hand the owner the landing page and
	// leave them wondering where their reader went.
	w.Header().Add("Vary", "Cookie")
	s.serve(w, r, s.shellFor(r, clean))
}

// shellFor picks which island's document a navigation gets. The whole of the routing
// between them, deliberately: one table, checked in one place.
//
// clean has been through path.Clean, so "/manage/" arrives here as "/manage".
//
// # Why "/" reads the request and nothing else does
//
// Every other line here is a prefix and nothing more, which is the property worth keeping:
// what a path serves is a fact about the path. "/" is the exception because it is genuinely
// two pages. To somebody with an account it is their front page; to a stranger it is the only
// thing this instance has to say about what it is, and handing them the reader's shell means
// a bundle they cannot use, a 401, and a whole-document redirect to a login form — before a
// word about what they are looking at.
//
// The test is whether the request *carries* a session cookie, not whether that session is any
// good. Resolving one here would mean a database read on every visit to the front page and a
// sliding-expiry write inside a GET, to decide which HTML to send. A cookie that has expired
// therefore still gets the reader, which then does what it does today: asks /api/me, is
// refused, and sends them to the login island. That is the right fallback rather than a
// missed case — a stale cookie is rare, it self-corrects on the next navigation, and the
// alternative is paying for the rare case on every other request.
func (s *SPA) shellFor(r *http.Request, clean string) asset {
	switch {
	// The landing page, for somebody with no session to their name — unless this instance
	// has turned it off, in which case "/" is what it was before: the reader's shell, which
	// bounces them to the login form.
	case clean == "/" && !carriesSession(r) && (s.landing == nil || s.landing(r)):
		return s.login
	case clean == LoginPath, strings.HasPrefix(clean, LoginPath+"/"):
		return s.login
	// The bare "/invite" case is the login island's too. It is what a truncated link looks
	// like — messaging apps cut long URLs — and that island answers with "this link looks
	// incomplete", which is both true and actionable. Falling through to the reader would
	// show a stranger the shell of an application they have no account for.
	case clean == strings.TrimSuffix(InvitePath, "/"), strings.HasPrefix(clean, InvitePath):
		return s.login
	case clean == ForgotPath:
		return s.login
	// The bare "/recover" too, and for the reason the bare "/invite" is here: a mail client
	// that wrapped a long URL leaves somebody holding half of one, and that island says so.
	case clean == strings.TrimSuffix(RecoverPath, "/"), strings.HasPrefix(clean, RecoverPath):
		return s.login
	case clean == ManagePath, strings.HasPrefix(clean, ManagePath+"/"):
		return s.manage
	case clean == SharePath, strings.HasPrefix(clean, SharePath+"/"):
		return s.manage
	case clean == AdminPath, strings.HasPrefix(clean, AdminPath+"/"):
		return s.admin
	// The bare "/p" goes here too, and answers "no page at this address" — which is what a
	// truncated link looks like, and is both true and actionable. Falling through to the
	// reader would show a stranger an application they have no account for.
	case clean == strings.TrimSuffix(PublicPath, "/"), strings.HasPrefix(clean, PublicPath):
		return s.public
	default:
		return s.index
	}
}

// carriesSession reports whether a request brought a session cookie at all.
//
// Presence, never validity — see shellFor. Named rather than inlined because "the visitor is
// a stranger" is the thing being decided, and `err == nil` at the call site says only that a
// cookie was found.
func carriesSession(r *http.Request) bool {
	cookie, err := r.Cookie(session.CookieName)
	return err == nil && cookie.Value != ""
}

func (s *SPA) serve(w http.ResponseWriter, r *http.Request, a asset) {
	h := w.Header()
	// Vary regardless of which representation this particular request gets: a cache that
	// stored the compressed bytes without it would later hand them to a client that cannot
	// read them.
	if a.gzipped {
		h.Set("Vary", "Accept-Encoding")
	}
	send := a.gzipped && acceptsGzip(r)

	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, a.etag) {
		h.Set("ETag", a.etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Opened before a single header goes out, so a file that cannot be read is an error
	// response rather than a 200 that stops halfway.
	var body io.Reader
	if r.Method != http.MethodHead {
		src, done, err := s.open(a, send)
		if err != nil {
			s.log.Error("could not read a file the bundle was indexed with; has it changed since startup?",
				"file", a.file, "error", err)
			writeError(w, http.StatusInternalServerError, "could not read "+r.URL.Path)
			return
		}
		defer done()
		body = src
	}

	h.Set("ETag", a.etag)
	h.Set("Content-Type", a.ctype)
	length := a.plain
	if send {
		h.Set("Content-Encoding", "gzip")
		length = a.size
	}
	h.Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusOK)

	if body != nil {
		// Nothing to be done about a failure part way through: the status has gone. It is
		// nearly always the client leaving, which is not worth more than a debug line.
		if _, err := io.Copy(w, body); err != nil {
			s.log.Debug("stopped sending a file", "file", a.file, "error", err)
		}
	}
}

// open is a file in the form this response wants: as stored, or decompressed on the way out.
//
// done closes whatever was opened, and is always safe to call.
func (s *SPA) open(a asset, asStored bool) (io.Reader, func(), error) {
	if a.file == "" {
		return bytes.NewReader(a.inline), func() {}, nil
	}
	f, err := s.dist.Open(a.file)
	if err != nil {
		return nil, nil, err
	}
	if !a.gzipped || asStored {
		return f, func() { f.Close() }, nil
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return z, func() { z.Close(); f.Close() }, nil
}

// acceptsGzip is a substring check rather than a full Accept-Encoding parse.
//
// The only thing that could go wrong is `gzip;q=0`, which means "explicitly do not send me
// gzip" — vanishingly rare, and every browser made this century sends a plain `gzip`.
// Handling it properly costs a parser; getting it wrong costs one unreadable response to a
// client that went out of its way to ask.
func acceptsGzip(r *http.Request) bool {
	ae := r.Header.Get("Accept-Encoding")
	return strings.Contains(ae, "gzip") && !strings.Contains(ae, "gzip;q=0")
}

// looksLikeNavigation distinguishes "the browser is loading a page" from "the page is
// loading a resource that does not exist".
func looksLikeNavigation(r *http.Request, clean string) bool {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		return true
	}
	// No extension on the last segment: /login, /manage/feeds — a route.
	return path.Ext(clean) == ""
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// contentType is an explicit table rather than mime.TypeByExtension, which reads
// /etc/mime.types and therefore answers differently on a scratch container than on the
// machine the code was tested on.
func contentType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".map":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
