package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"bystander/internal/feeds"
	"bystander/internal/store"
)

// proxyBody is a relay as an administrator sees it. No token: it is write-only, so a page that
// renders the list cannot hand the credentials back out again.
type proxyBody struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
	// HasToken says a token is stored without saying what it is, so the form can show that
	// one is set and let it be left alone.
	HasToken bool `json:"has_token"`
	// Routes is how many publishers are currently reached through this relay. What makes
	// "delete" and "reset" something other than a guess.
	Routes    int   `json:"routes"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// summaryOf drops the token from a relay that has just been written, so the reply goes through
// exactly the same shape the list does rather than a second one that could drift.
func summaryOf(p *store.Proxy) *store.ProxySummary {
	return &store.ProxySummary{
		ID: p.ID, Kind: p.Kind, Label: p.Label, URL: p.URL, Username: p.Username,
		Priority: p.Priority, Enabled: p.Enabled,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func proxyOut(p *store.ProxySummary) proxyBody {
	return proxyBody{
		ID: p.ID, Kind: string(p.Kind), Label: p.Label, URL: p.URL, Username: p.Username,
		Priority: p.Priority, Enabled: p.Enabled, HasToken: true,
		CreatedAt: p.CreatedAt.Unix(), UpdatedAt: p.UpdatedAt.Unix(),
	}
}

func (s *Server) listProxies(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ProxySummaries(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// The relays live in main.db and what they carry lives in derived.db, so this is two
	// reads joined here rather than one query. See internal/store for why no join can cross.
	counts, err := s.store.ProxyRouteCounts(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]proxyBody, 0, len(list))
	for _, p := range list {
		body := proxyOut(p)
		body.Routes = counts[p.ID]
		out = append(out, body)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proxies": out})
}

// resetProxy forgets every publisher this relay had been learned as the way to.
//
// Routes end on their own when a relay fails, which covers everything being wrong. This covers
// the operator knowing something the instance cannot: a restriction lifted, a relay moved, a
// setup that was only ever a test. The relay itself is untouched — this is about what was
// learned through it, not about whether to keep it.
func (s *Server) resetProxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Read first, so resetting something that is not there is a 404 rather than a cheerful
	// report of nothing having been forgotten.
	if _, err := s.store.ProxyByID(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	forgotten, err := s.store.ForgetProxyRoutes(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"forgotten": forgotten})
}

type proxyRequest struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	Username string `json:"username"`
	// Token is proxio's token or SOCKS5's password. Empty on an edit means "the one already
	// stored", which is what makes it possible to correct a label without the browser ever
	// having been sent the credential to send back.
	Token string `json:"token"`
	// Priority is a pointer for the same reason Enabled is: a relay left out of the body should
	// keep what it had, and a missing JSON number and a zero one are the same value once
	// decoded — and zero here is a real setting meaning "tried last".
	Priority *int  `json:"priority"`
	Enabled  *bool `json:"enabled"`
}

// proxy is the request as the store wants it, before any of it is checked.
//
// The two optional fields take what the caller has decided they fall back to, which for an edit
// is whatever is stored and for a new relay is the default. Both are pointers in the request
// because a missing JSON value and a zero one decode identically, and both zeroes mean
// something here: off, and tried last.
func (b proxyRequest) proxy(enabled bool, priority int) store.Proxy {
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	if b.Priority != nil {
		priority = *b.Priority
	}
	return store.Proxy{
		Kind: store.ProxyKind(strings.TrimSpace(b.Kind)), Label: b.Label, URL: b.URL,
		Username: b.Username, Token: b.Token, Priority: priority, Enabled: enabled,
	}
}

// DefaultProxyPriority is what a relay is worth when nobody says.
//
// The top of the range rather than the middle. A relay somebody has just gone to the trouble of
// configuring is one they want used, and the ordering only starts to matter once there are two
// — at which point they are equal and created_at settles it, which is the same answer as having
// been asked and not cared.
const DefaultProxyPriority = 100

func (s *Server) addProxy(w http.ResponseWriter, r *http.Request) {
	var body proxyRequest
	if !decode(w, r, &body) {
		return
	}
	// A relay added without saying is a relay somebody wants used.
	in := body.proxy(true, DefaultProxyPriority)
	if in.Kind == "" {
		in.Kind = store.ProxioKind
	}
	added, err := s.store.AddProxy(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, proxyOut(summaryOf(added)))
}

// putProxy replaces one relay's settings.
//
// An empty token means the one already stored, for the same reason the SMTP password does: it
// is never sent to the browser, so requiring it on every save would mean retyping a secret
// nobody can see in order to correct a label.
func (s *Server) putProxy(w http.ResponseWriter, r *http.Request) {
	var body proxyRequest
	if !decode(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	existing, err := s.store.ProxyByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	in := body.proxy(existing.Enabled, existing.Priority)
	if in.Kind == "" {
		in.Kind = existing.Kind
	}
	updated, err := s.store.UpdateProxy(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, proxyOut(summaryOf(updated)))
}

func (s *Server) deleteProxy(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProxy(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// proxyProbe is what a relay is asked to fetch when somebody presses Test.
//
// This instance's own address. It is certain to exist, it is not somebody else's server being
// used as a test target, and a relay that can reach it has demonstrated the whole path:
// resolving a name, making the request, and handing back what came out.
const proxyProbeTimeout = 20 * time.Second

type testProxyRequest struct {
	// ID names a stored relay. Optional: a relay that has not been saved yet has no id, and
	// being able to try one before committing to it is the whole point.
	ID string `json:"id"`
	// Proxy, when given, is tried instead of whatever is stored and nothing is written.
	//
	// With an ID beside it, an empty token means the stored one — so an address can be
	// corrected and tried without retyping a credential the browser was never sent.
	Proxy *proxyRequest `json:"proxy"`
	// URL is what to ask the relay to fetch. Defaults to this instance's own address.
	URL string `json:"url"`
}

// testProxy asks one relay to fetch something and says what came back.
//
// It relays rather than merely connecting, because the two fail differently: a relay will
// happily accept a connection and then refuse the token, and an operator who saw "reachable"
// would find that out later, from a feed that quietly stopped updating.
//
// No id in the path, so a relay that does not exist yet can be tried. That is what the whole
// thing is for — the only way to find out whether a token works should not be to save it over
// one that already did.
func (s *Server) testProxy(w http.ResponseWriter, r *http.Request) {
	var body testProxyRequest
	if !decode(w, r, &body) {
		return
	}

	proxy, ok := s.proxyToTry(w, r, body)
	if !ok {
		return
	}

	target := strings.TrimSpace(body.URL)
	if target == "" {
		// This instance's own address: certain to exist, not somebody else's server pressed
		// into service as a test target, and reaching it exercises the whole path — resolving
		// a name, making the request, handing back what came out.
		target = s.cfg.PublicURL.String()
	}

	ctx, cancel := context.WithTimeout(r.Context(), proxyProbeTimeout)
	defer cancel()

	status, err := feeds.TestProxy(ctx, proxy, target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": err.Error(),
			"url":   target,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": status,
		"url":    target,
	})
}

// proxyToTry is the relay a test should use: the one in the request, the stored one, or both
// where the request is a stored relay being edited.
//
// Reports false when it has already written a response.
func (s *Server) proxyToTry(w http.ResponseWriter, r *http.Request, body testProxyRequest) (*store.Proxy, bool) {
	var stored *store.Proxy
	if body.ID != "" {
		found, err := s.store.ProxyByID(r.Context(), body.ID)
		if err != nil {
			s.fail(w, r, err)
			return nil, false
		}
		stored = found
	}

	if body.Proxy == nil {
		if stored == nil {
			writeError(w, http.StatusBadRequest, "there is no relay here to try")
			return nil, false
		}
		return stored, true
	}

	enabled, priority := true, DefaultProxyPriority
	if stored != nil {
		enabled, priority = stored.Enabled, stored.Priority
	}
	candidate := body.Proxy.proxy(enabled, priority)
	if candidate.Kind == "" && stored != nil {
		candidate.Kind = stored.Kind
	}
	if candidate.Kind == "" {
		candidate.Kind = store.ProxioKind
	}
	// Same rule as saving: an empty token means the one already there, so a relay whose
	// address has been corrected can be tried without retyping a credential.
	if strings.TrimSpace(candidate.Token) == "" && stored != nil {
		candidate.Token = stored.Token
	}
	checked, err := store.ValidateProxy(candidate)
	if err != nil {
		s.fail(w, r, err)
		return nil, false
	}
	if stored != nil {
		checked.ID = stored.ID
	}
	return &checked, true
}
