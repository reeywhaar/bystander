// Package api serves bystander: a login, a front page, the pages that manage it, and the
// bundle that renders them.
//
// # Refusals are honest
//
// This service serves a login page at /. It announces what it is by existing, so there is
// nothing to disguise and no reason to collapse every refusal into one padded 404 the way
// a service that must pass for something else would. Handlers here return real 401s, 403s
// and 409s, and the interface's error handling is the better for it.
//
// # There is no CORS middleware, deliberately
//
// The browser only ever talks to this origin. Adding CORS here would weaken two of the
// three CSRF defences in csrfGuard, so its absence is load-bearing rather than an
// oversight. There is a test asserting no Access-Control-Allow-Origin is ever emitted.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"bystander/internal/app"
	"bystander/internal/config"
	"bystander/internal/edition"
	"bystander/internal/feeds"
	mailer "bystander/internal/mail"
	"bystander/internal/session"
	"bystander/internal/store"
)

// Server holds everything the handlers need.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	sessions *session.Table
	gen      *edition.Generator
	fetcher  *feeds.Fetcher
	spa      *SPA
	log      *slog.Logger

	logins    *limiter
	discovery *limiter
	mail      *limiter

	// sendMail is how a message leaves.
	//
	// A field rather than a direct call, so a test can watch what would have been sent
	// without standing up a relay with a certificate of its own. What happens on the wire
	// is internal/mail's to prove, and it proves it against a real one.
	sendMail func(context.Context, mailer.Settings, mailer.Message) error
}

// New builds a server.
func New(cfg *config.Config, st *store.Store, sessions *session.Table, gen *edition.Generator,
	fetcher *feeds.Fetcher, spa *SPA, log *slog.Logger) *Server {
	return &Server{
		cfg:      cfg,
		store:    st,
		sessions: sessions,
		gen:      gen,
		fetcher:  fetcher,
		spa:      spa,
		log:      log,
		// Bcrypt at cost 12 on an unauthenticated endpoint is a CPU exhaustion vector
		// before it is an authentication one, so the login limit is global as well as
		// per-name.
		logins: newLimiter(10, time.Minute),
		// Adding a feed makes an outbound request on the caller's behalf. An endpoint
		// that fetches an arbitrary URL for whoever asks needs a ceiling.
		discovery: newLimiter(20, time.Minute),
		// A test send is an outbound connection to somebody else's relay, made on
		// demand. Administrators are trusted, and relays still have rate limits of
		// their own worth staying under.
		mail:     newLimiter(5, time.Minute),
		sendMail: mailer.Send,
	}
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)

	// Unauthenticated. Everything else on /api requires a session.
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/invites/{token}", s.invite)
	mux.HandleFunc("POST /api/invites/{token}/accept", s.acceptInvite)

	mux.HandleFunc("POST /api/logout", s.logout)
	mux.Handle("GET /api/me", s.requireSession(s.me))

	mux.Handle("GET /api/account", s.requireSession(s.account))
	mux.Handle("POST /api/account/password", s.requireSession(s.changePassword))
	mux.Handle("POST /api/account/recovery", s.requireSession(s.beginRecovery))
	mux.Handle("POST /api/account/recovery/confirm", s.requireSession(s.confirmRecovery))
	mux.Handle("DELETE /api/account/recovery", s.requireSession(s.clearRecovery))

	mux.Handle("GET /api/pages", s.requireSession(s.listPages))
	mux.Handle("POST /api/pages", s.requireSession(s.createPage))
	mux.Handle("GET /api/pages/{id}", s.requireSession(s.getPage))
	mux.Handle("PATCH /api/pages/{id}", s.requireSession(s.patchPage))
	mux.Handle("DELETE /api/pages/{id}", s.requireSession(s.deletePage))

	// Both of these take an optional ?page=, by id or by address. Without one they answer for
	// the main page, which is what the reader asks for at /.
	mux.Handle("GET /api/edition", s.requireSession(s.edition))
	mux.Handle("POST /api/edition/regenerate", s.requireSession(s.regenerate))
	mux.Handle("GET /api/read", s.requireSession(s.readArticles))
	mux.Handle("PUT /api/edition/items/{id}/read", s.requireSession(s.markRead))
	mux.Handle("DELETE /api/edition/items/{id}/read", s.requireSession(s.markUnread))

	mux.Handle("GET /api/feeds", s.requireSession(s.listFeeds))
	mux.Handle("POST /api/feeds", s.requireSession(s.addFeed))
	mux.Handle("POST /api/feeds/discover", s.requireSession(s.discoverFeeds))
	mux.Handle("POST /api/feeds/preview", s.requireSession(s.previewFeed))
	mux.Handle("POST /api/feeds/export", s.requireSession(s.exportFeeds))
	mux.Handle("POST /api/feeds/import/preview", s.requireSession(s.previewImport))
	mux.Handle("POST /api/feeds/import", s.requireSession(s.importFeeds))

	mux.Handle("POST /api/shares", s.requireSession(s.createShare))
	// A session, not a public page. A share is a list of what somebody reads, handed to
	// another person on this instance — not published to whoever finds the URL.
	mux.Handle("GET /api/shares/{token}", s.requireSession(s.share))
	mux.Handle("GET /api/feeds/{id}", s.requireSession(s.getFeed))
	mux.Handle("PATCH /api/feeds/{id}", s.requireSession(s.patchFeed))
	mux.Handle("POST /api/feeds/{id}/read", s.requireSession(s.markFeedRead))
	mux.Handle("DELETE /api/feeds/{id}/read", s.requireSession(s.unmarkFeedRead))
	mux.Handle("DELETE /api/feeds/{id}", s.requireSession(s.deleteFeed))

	mux.Handle("GET /api/tags", s.requireSession(s.listTags))
	mux.Handle("POST /api/tags", s.requireSession(s.addTag))
	mux.Handle("PATCH /api/tags/{id}", s.requireSession(s.patchTag))
	mux.Handle("DELETE /api/tags/{id}", s.requireSession(s.deleteTag))

	mux.Handle("GET /api/admin/users", s.requireAdmin(s.listUsers))
	mux.Handle("PATCH /api/admin/users/{id}", s.requireAdmin(s.patchUser))
	mux.Handle("DELETE /api/admin/users/{id}", s.requireAdmin(s.deleteUser))
	mux.Handle("GET /api/admin/invites", s.requireAdmin(s.listInvites))
	mux.Handle("POST /api/admin/invites", s.requireAdmin(s.createInvite))
	mux.Handle("DELETE /api/admin/invites/{id}", s.requireAdmin(s.deleteInvite))
	mux.Handle("GET /api/admin/smtp", s.requireAdmin(s.getSMTP))
	mux.Handle("PUT /api/admin/smtp", s.requireAdmin(s.putSMTP))
	mux.Handle("DELETE /api/admin/smtp", s.requireAdmin(s.deleteSMTP))
	mux.Handle("POST /api/admin/smtp/test", s.requireAdmin(s.testSMTP))
	mux.Handle("GET /api/admin/images", s.requireAdmin(s.images))
	mux.Handle("POST /api/admin/images/retry", s.requireAdmin(s.retryImages))

	// A mistyped API path must never fall through to the SPA: an HTML document returned
	// to a fetch presents as a JSON parse error with no hint of what actually happened.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint: "+r.Method+" "+r.URL.Path)
	})

	mux.Handle("/", s.spa)

	return s.csrfGuard(mux)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": app.Version})
}

// principalKey carries the signed-in account down to the handler.
type principalKey struct{}

// principalOf returns the account this request is signed in as. Only ever called from a
// handler behind requireSession or requireAdmin, where it cannot be nil.
func principalOf(r *http.Request) *store.Principal {
	p, _ := r.Context().Value(principalKey{}).(*store.Principal)
	return p
}

// requireSession refuses anybody who is not signed in.
//
// Authorisation is decided here, at registration, rather than inside each handler. A
// handler cannot forget to check, because one registered without this is visibly
// registered without it.
func (s *Server) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.sessions.Resolve(r.Context(), w, r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if p == nil {
			// 401 and a JSON body, never a redirect: a 302 to an HTML page is the least
			// useful thing a fetch can receive.
			writeError(w, http.StatusUnauthorized, "sign in to do that")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// requireAdmin refuses anybody who is not an administrator.
func (s *Server) requireAdmin(next http.HandlerFunc) http.Handler {
	return s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r).Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "that is an administrator's to do")
			return
		}
		next(w, r)
	})
}

// csrfGuard is three cheap checks, and together they are the whole defence.
//
// The cookie is SameSite=Lax, which is the first. This adds the other two: a mutating
// request that announces itself as cross-site is refused, and one carrying a body must
// declare JSON — which a form post, the shape most cross-site attempts take, cannot do.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions

		if !safe {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				writeError(w, http.StatusForbidden, "cross-site requests are not accepted")
				return
			}
			// Only when a body is actually present: a DELETE legitimately carries none.
			if ct := r.Header.Get("Content-Type"); ct != "" || r.ContentLength > 0 {
				if !isJSON(ct) {
					writeError(w, http.StatusUnsupportedMediaType, "request bodies must be application/json")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isJSON ignores the parameters after a content type, so "application/json; charset=utf-8"
// is the same declaration as "application/json".
func isJSON(contentType string) bool {
	base, _, _ := strings.Cut(contentType, ";")
	return strings.TrimSpace(base) == "application/json"
}
