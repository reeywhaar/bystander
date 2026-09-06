package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"bystander/internal/ids"
)

// ProxyKind is how a relay is spoken to.
//
// Two, and they are not variations on each other — see the two constants. What they share is
// being one address with one credential that a request can be sent through, which is why one
// table holds both.
type ProxyKind string

const (
	// ProxioKind relays through https://github.com/reeywhaar/proxio: `GET /proxy?url=…&token=…`,
	// which hands back whatever the target said. The request is rewritten and the connection is
	// an ordinary one, so it needs nothing but a URL and a token.
	ProxioKind ProxyKind = "proxio"

	// SocksKind dials through a SOCKS5 endpoint.
	//
	// A different layer: the request is untouched and the *connection* is made somewhere else,
	// so it needs its own dialer rather than its own URL. Authenticates with a username and a
	// password where proxio takes one opaque token — see [Proxy.Username].
	SocksKind ProxyKind = "socks5"
)

// Valid reports whether k is a kind this program knows how to dial.
func (k ProxyKind) Valid() bool { return k == ProxioKind || k == SocksKind }

// NeedsUsername reports whether this kind authenticates with a name as well as a secret.
func (k ProxyKind) NeedsUsername() bool { return k == SocksKind }

// Scheme is what a relay of this kind's address must be written as.
func (k ProxyKind) Scheme() []string {
	if k == SocksKind {
		return []string{"socks5", "socks5h"}
	}
	return []string{"http", "https"}
}

// Proxy is one relay, token included. See [ProxySummary] for the shape that is safe to show.
type Proxy struct {
	ID    string
	Kind  ProxyKind
	Label string
	// URL is the relay's own address with no path — how a request is built out of it is the
	// kind's business. See internal/feeds.
	URL string

	// Username is empty for kinds that authenticate with a secret alone, which is proxio.
	// SOCKS5 wants a name beside the password.
	Username string

	// Token is the secret: proxio's token, or SOCKS5's password.
	//
	// Never in URL, which is what an administrator's browser is shown. `socks5://user:pass@host`
	// is the natural way to write one of these down and it would put the password on screen.
	Token string

	// Priority is the order they are tried in, highest first.
	//
	// 0..100, the same scale and direction as a feed's, because two numbers in one product
	// that both run 0..100 and disagree about which end is which would be a small cruelty. It
	// is not the same kind of number — a feed's is a probability, this is an ordering — and 0
	// here means tried last rather than never. Never is [Proxy.Enabled].
	Priority int
	Enabled  bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Name is what to call this relay in a log line: its label, or its address if it has none.
func (p *Proxy) Name() string {
	if p.Label != "" {
		return p.Label
	}
	return p.URL
}

// ProxySummary is a relay as it is safe to hand to a browser: everything except the token.
//
// Its own type rather than a Proxy with the field blanked, for the same reason SMTPSummary is:
// a handler that renders one then cannot serialize the other by forgetting a line.
type ProxySummary struct {
	ID        string
	Kind      ProxyKind
	Label     string
	URL       string
	Username  string
	Priority  int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProxySummaries is every relay, in the order they are tried, without their tokens.
func (s *Store) ProxySummaries(ctx context.Context) ([]*ProxySummary, error) {
	rows, err := s.main.QueryContext(ctx, `
		SELECT id, kind, label, url, username, priority, enabled, created_at, updated_at
		  FROM proxies ORDER BY priority DESC, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*ProxySummary{}
	for rows.Next() {
		var (
			p                ProxySummary
			kind             string
			created, updated int64
		)
		if err := rows.Scan(&p.ID, &kind, &p.Label, &p.URL, &p.Username, &p.Priority, &p.Enabled,
			&created, &updated); err != nil {
			return nil, err
		}
		p.Kind = ProxyKind(kind)
		p.CreatedAt, p.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
		out = append(out, &p)
	}
	return out, rows.Err()
}

// Proxies is every relay that is switched on, in the order they are tried, tokens included.
//
// Separate from [Store.ProxySummaries] so that showing a relay and using one are different
// calls. Disabled rows are left out here rather than filtered by the caller: a caller that
// forgets is a caller that quietly keeps using a relay somebody turned off.
func (s *Store) Proxies(ctx context.Context) ([]*Proxy, error) {
	rows, err := s.main.QueryContext(ctx, `
		SELECT id, kind, label, url, username, token, priority, created_at, updated_at
		  FROM proxies WHERE enabled = 1 ORDER BY priority DESC, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Proxy{}
	for rows.Next() {
		var (
			p                = Proxy{Enabled: true}
			kind             string
			created, updated int64
		)
		if err := rows.Scan(&p.ID, &kind, &p.Label, &p.URL, &p.Username, &p.Token, &p.Priority,
			&created, &updated); err != nil {
			return nil, err
		}
		p.Kind = ProxyKind(kind)
		p.CreatedAt, p.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ProxyByID is one relay with its token, for a test that has to actually dial it.
func (s *Store) ProxyByID(ctx context.Context, id string) (*Proxy, error) {
	var (
		p                Proxy
		kind             string
		created, updated int64
	)
	err := s.main.QueryRowContext(ctx, `
		SELECT id, kind, label, url, username, token, priority, enabled, created_at, updated_at
		  FROM proxies WHERE id = ?`, id).
		Scan(&p.ID, &kind, &p.Label, &p.URL, &p.Username, &p.Token, &p.Priority, &p.Enabled,
			&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no proxy %s", id)
	}
	if err != nil {
		return nil, err
	}
	p.Kind = ProxyKind(kind)
	p.CreatedAt, p.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
	return &p, nil
}

// ValidateProxy checks a relay over and hands back the tidied version.
//
// Exported so a handler can refuse a bad one with the same words the store would, and so the
// rules are readable in one place rather than spread across a form and a table constraint.
func ValidateProxy(in Proxy) (Proxy, error) {
	if !in.Kind.Valid() {
		return in, Invalid("%q is not a kind of proxy this knows about", in.Kind)
	}

	// The same addresses the form offers, so a refusal and a placeholder do not disagree
	// about what one of these looks like.
	schemes := in.Kind.Scheme()
	example := "socks5://socks.example.com:1080"
	if in.Kind == ProxioKind {
		example = "https://proxio.example.com"
	}

	raw := strings.TrimSpace(in.URL)
	if raw == "" {
		return in, Invalid("a proxy needs an address, e.g. %s", example)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return in, Invalid("%q is not a URL: %v", raw, err)
	}
	if parsed.Scheme == "" {
		return in, Invalid("%q has no scheme; write it in full, e.g. %s://%s",
			raw, schemes[0], raw)
	}
	if !slices.Contains(schemes, strings.ToLower(parsed.Scheme)) {
		return in, Invalid("a %s proxy's address is %s, not %q",
			in.Kind, strings.Join(schemes, " or "), parsed.Scheme)
	}
	if parsed.Host == "" {
		return in, Invalid("%q names no host", raw)
	}
	// Credentials belong in their own columns, not in the address that gets shown and logged.
	// Somebody who pasted `socks5://user:pass@host` gets the parts put where they belong
	// rather than a refusal or a password on screen.
	if parsed.User != nil {
		if in.Username == "" {
			in.Username = parsed.User.Username()
		}
		if pass, ok := parsed.User.Password(); ok && in.Token == "" {
			in.Token = pass
		}
		parsed.User = nil
	}
	// The path a relay serves on is the kind's business, so anything typed after the host is
	// dropped rather than kept and quietly ignored — somebody who pasted the whole
	// `/proxy?url=…` example should get back an address that works, not one that half does.
	parsed.Path, parsed.RawQuery, parsed.Fragment = "", "", ""
	parsed.Scheme, parsed.Host = strings.ToLower(parsed.Scheme), strings.ToLower(parsed.Host)
	in.URL = parsed.String()

	if strings.TrimSpace(in.Token) == "" {
		what := "a token"
		if in.Kind.NeedsUsername() {
			what = "a password"
		}
		return in, Invalid("a %s proxy needs %s; delete the whole entry instead", in.Kind, what)
	}
	in.Token = strings.TrimSpace(in.Token)
	in.Username = strings.TrimSpace(in.Username)
	if in.Kind.NeedsUsername() && in.Username == "" {
		return in, Invalid("a %s proxy needs a username", in.Kind)
	}
	if !in.Kind.NeedsUsername() {
		// Not merely ignored: a name stored against a kind that never reads one is a field
		// somebody will one day believe is doing something.
		in.Username = ""
	}
	if in.Priority < 0 || in.Priority > 100 {
		return in, Invalid("priority must be between 0 and 100, not %d", in.Priority)
	}
	in.Label = strings.TrimSpace(in.Label)
	return in, nil
}

// AddProxy stores a new relay and returns it.
func (s *Store) AddProxy(ctx context.Context, in Proxy) (*Proxy, error) {
	in, err := ValidateProxy(in)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	in.ID = ids.New(ids.Proxy)
	in.CreatedAt, in.UpdatedAt = now.UTC(), now.UTC()
	if _, err := s.main.ExecContext(ctx, `
		INSERT INTO proxies
			(id, kind, label, url, username, token, priority, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ID, string(in.Kind), in.Label, in.URL, in.Username, in.Token, in.Priority, in.Enabled,
		now.Unix(), now.Unix()); err != nil {
		return nil, err
	}
	return &in, nil
}

// UpdateProxy replaces a relay's settings.
//
// An empty token means "leave the one that is there", which is what makes it possible to edit a
// label without the browser ever having been sent the credential to send back.
func (s *Store) UpdateProxy(ctx context.Context, id string, in Proxy) (*Proxy, error) {
	existing, err := s.ProxyByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Token) == "" {
		in.Token = existing.Token
	}
	in, err = ValidateProxy(in)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if _, err := s.main.ExecContext(ctx, `
		UPDATE proxies SET kind = ?, label = ?, url = ?, username = ?, token = ?, priority = ?,
		                   enabled = ?, updated_at = ?
		 WHERE id = ?`,
		string(in.Kind), in.Label, in.URL, in.Username, in.Token, in.Priority, in.Enabled,
		now.Unix(), id); err != nil {
		return nil, err
	}
	in.ID, in.CreatedAt, in.UpdatedAt = id, existing.CreatedAt, now.UTC()
	return &in, nil
}

// DeleteProxy forgets a relay, token and all, and everything learned through it.
//
// The routes go too. Nothing breaks if they do not — a route naming a relay that is gone is
// ignored, which it has to be, since no foreign key can cross between the two databases — but
// they would sit there forever naming an id nothing can resolve, and the next relay to be added
// would look like it was carrying publishers it had never reached.
//
// Not one transaction, because it cannot be: the two databases are separate handles and a
// transaction spanning them is a transaction only on paper under WAL. The relay goes first, so
// a failure between them leaves orphaned routes rather than a relay nothing routes to — the
// harmless way round, and the one the readers already tolerate.
func (s *Store) DeleteProxy(ctx context.Context, id string) error {
	res, err := s.main.ExecContext(ctx, `DELETE FROM proxies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return NotFound("no proxy %s", id)
	}
	if _, err := s.ForgetProxyRoutes(ctx, id); err != nil {
		return err
	}
	return nil
}

// ProxyRoute is which relay last reached a domain, and when that was confirmed.
//
// Empty id and a zero time when nothing has been learned about it. Not an error: never having
// needed a relay for a publisher is the ordinary case, and it is the same answer as having
// forgotten one.
func (s *Store) ProxyRoute(ctx context.Context, domain string) (string, time.Time, error) {
	var (
		id      string
		updated int64
	)
	err := s.derived.QueryRowContext(ctx,
		`SELECT proxy_id, updated_at FROM proxy_routes WHERE domain = ?`, domain).
		Scan(&id, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return id, time.Unix(updated, 0).UTC(), nil
}

// RememberProxyRoute records that a relay reached a domain, and stamps it as of now.
//
// Stamped on every success rather than only on the first, so a route in daily use stays fresh
// and one that has stopped being used ages out. See feeds.routeTTL for what that buys.
func (s *Store) RememberProxyRoute(ctx context.Context, domain, proxyID string) error {
	_, err := s.derived.ExecContext(ctx, `
		INSERT INTO proxy_routes (domain, proxy_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (domain) DO UPDATE SET
			proxy_id = excluded.proxy_id,
			updated_at = excluded.updated_at`,
		domain, proxyID, time.Now().Unix())
	return err
}

// ForgetProxyRoute drops what was learned about a domain, so the next request works it out
// again from the top.
func (s *Store) ForgetProxyRoute(ctx context.Context, domain string) error {
	_, err := s.derived.ExecContext(ctx, `DELETE FROM proxy_routes WHERE domain = ?`, domain)
	return err
}

// ForgetProxyRoutes drops everything learned through one relay, and says how much that was.
//
// The escape hatch for a route being permanent. Routes end on their own when a relay fails, and
// that covers the case where something is wrong — but not the case where an operator knows
// something the instance cannot: a publisher has lifted a restriction, a relay has moved
// countries, the whole thing was set up to test and is now in the way. Per relay rather than
// all at once, because "this one is no longer the right way round" is the shape of that
// knowledge.
func (s *Store) ForgetProxyRoutes(ctx context.Context, proxyID string) (int64, error) {
	res, err := s.derived.ExecContext(ctx,
		`DELETE FROM proxy_routes WHERE proxy_id = ?`, proxyID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ProxyRouteCounts is how many publishers are reached through each relay, by relay id.
//
// So a relay can say what it is carrying. A list of endpoints with no idea which of them
// anything depends on makes "delete" and "reset" into guesses.
func (s *Store) ProxyRouteCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.derived.QueryContext(ctx,
		`SELECT proxy_id, COUNT(*) FROM proxy_routes GROUP BY proxy_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var (
			id string
			n  int
		)
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
