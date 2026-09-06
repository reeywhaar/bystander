package feeds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/publicsuffix"

	"bystander/internal/store"
)

// blockedStatuses are the answers a proxy might get past.
//
// A publisher that is geo-fenced, behind a bot wall, or rate-limiting this instance's address
// says so with one of these, and the same request from somewhere else may well be answered.
// Everything else is the publisher's own answer about the URL rather than about who is asking:
// a 404 is missing from any address, a 500 is broken from any address, and retrying either
// through every configured relay would cost a request per relay per fetch cycle, forever, to
// arrive at the answer already in hand.
var blockedStatuses = map[int]bool{
	http.StatusForbidden:                  true, // 403, the ordinary geo-block and bot wall
	http.StatusUnavailableForLegalReasons: true, // 451
	http.StatusTooManyRequests:            true, // 429, this address specifically
}

// worthRetrying reports whether a relay is worth trying after this attempt.
//
// A transport error means nothing was reached at all — DNS, a refused connection, a timeout, a
// TLS handshake — and is the case relays exist for. A status is only worth another try when it
// is about the caller rather than about the URL.
func worthRetrying(status int, err error) bool {
	if err != nil {
		return true
	}
	return blockedStatuses[status]
}

// proxyError is the header proxio sets when the failure is its own rather than the target's.
//
// Load-bearing, and not merely for the log line. Without it the two are indistinguishable: a
// relay refusing a stale token answers 401 and a publisher demanding a login answers 401, and
// since 401 is not a status worth retrying, the relay's own refusal would be handed back as the
// publisher's answer — recorded against the feed, shown beside it in the interface, and
// stopping every relay after it from being tried. One mistyped token would quietly break every
// feed that needed a relay.
const proxyError = "X-Proxio-Error"

// through rewrites a request to go via one relay.
//
// The relay's stored address is a host and nothing else — see [store.ValidateProxy] — so the
// path and the shape of the query live here, where the kind is known. proxio takes the target
// as a percent-encoded `url` and its credential as `token`; url.Values does the encoding, so a
// target with its own query string survives being nested inside one.
//
// The method, headers and body are the caller's: proxio inherits them, which is what makes a
// conditional request still conditional through a relay. The URL is the only thing that
// changes, so the request that goes out is the request that would have gone out.
func through(req *http.Request, proxy *store.Proxy) (*http.Request, error) {
	target, err := url.Parse(proxy.URL)
	if err != nil {
		return nil, fmt.Errorf("%s has an address that will not parse: %w", proxy.Name(), err)
	}
	target.Path = "/proxy"
	target.RawQuery = url.Values{
		"url":   {req.URL.String()},
		"token": {proxy.Token},
		// The instance's own address is nobody's business but ours: without this, proxio
		// appends an X-Forwarded-For naming the machine the relay was there to stand in for.
		"hide": {"1"},
	}.Encode()

	out := req.Clone(req.Context())
	out.URL = target
	out.Host = ""
	return out, nil
}

// Relays is where the list of relays to fall back on comes from, in the order to try them.
//
// A function rather than a list, because an administrator adds and removes relays while this is
// running, and a list read at startup is a list that needs a restart to change. Called only on
// the request that needs one — a fetch that succeeds directly never asks.
type Relays func(context.Context) ([]*store.Proxy, error)

// Routes remembers which relay last reached a publisher.
//
// An interface rather than the store, so the ladder below can be tested without a database and
// so this package keeps not knowing how anything is stored.
type Routes interface {
	ProxyRoute(ctx context.Context, domain string) (proxyID string, at time.Time, err error)
	RememberProxyRoute(ctx context.Context, domain, proxyID string) error
	ForgetProxyRoute(ctx context.Context, domain string) error
}

// transient is a refusal that says "not right now" rather than "not from you".
//
// It matters because a route is a *permanent* decision, and one refusal does not deserve to
// become one. A publisher restricted by region is restricted by region — that is its licensing,
// not a state that lifts — so learning to reach it through a relay is learning the way there.
// Being rate-limited for an afternoon is not.
//
// The distinction is only needed for this one status, and that is worth saying because it looks
// like it should need more. A route can only ever be created when the direct request failed
// *and* a relay succeeded, and a transient fault fails both ways: a publisher having a bad hour
// refuses the relay too, so nothing is written down. Every other refusal that gets as far as
// creating a route is therefore already structural by construction. 429 is the exception
// because a rate limit is counted per caller, so the relay sails through and the instance
// learns a permanent lesson from a temporary one.
func transient(status int, err error) bool {
	return err == nil && status == http.StatusTooManyRequests
}

// domainOf is the key a route is remembered under: the registrable domain, not the host.
//
// A publisher serving its feed from www and its pictures from an images subdomain is one
// publisher with one block, and learning those separately means paying for the lesson twice.
// eTLD+1 is what makes that generalisation safe — it groups foo.example.com with
// bar.example.com and does not group two unrelated sites sharing .co.uk.
//
// Falls back to the host for anything the list does not cover, which is an IP address or a
// name inside a private network. Those are each themselves, which is the right answer for them.
func domainOf(u *url.URL) string {
	host := u.Hostname()
	if host == "" {
		return ""
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return domain
}

// relay makes a request the best way it knows, and works its way down if that fails.
//
// The order is: whatever reached this publisher last time, then directly, then each relay in
// turn. Everything below is why it is that order rather than a simpler one.
//
// **The remembered relay goes first**, and skipping the direct request is the whole point of
// remembering. A publisher restricted by region is restricted every time, so on a feed checked
// hourly the direct attempt is a request an hour spent confirming something already known.
//
// The route has no expiry, because a region is not a state that lifts and a timer would spend a
// failed request per blocked domain per period finding that out. What ends a route is the relay
// failing: it is dropped there and then, and the very next thing tried is the direct request —
// so if the block has gone, that is when it is noticed and the route stays gone. The one
// refusal that could mint a permanent route out of a temporary state is a rate limit, and
// [transient] is why it does not.
//
// **Then directly**, and it always happens when there is no route. A relay is a way past a
// refusal, not a way of doing business: routing everything through one would put a third party
// in front of every publisher for the sake of the handful that block us.
//
// **Then the rest**, in the order the administrator put them in.
//
// The *first* answer is what comes back when nothing is worth retrying, and *also* what comes
// back when everything was. So a feed that 403s everywhere reports the publisher's 403 rather
// than whatever the last relay said about itself, and a feed that is simply gone reports being
// gone without a relay having been dialled at all.
//
// A free function rather than a method, because two callers want it with different clients:
// fetching a feed has thirty seconds and measuring a picture has five, and the timeout belongs
// to the caller's job rather than to the ladder.
func relay(ctx context.Context, client *http.Client, req *http.Request,
	relays Relays, routes Routes, log *slog.Logger) (*http.Response, error) {

	domain := domainOf(req.URL)

	// Loaded at most once, and only when something needs it.
	var proxies []*store.Proxy
	loaded := false
	list := func() []*store.Proxy {
		if !loaded {
			proxies, loaded = available(ctx, relays, log), true
		}
		return proxies
	}

	if known := remembered(ctx, routes, list, domain, log); known != nil {
		res, err := once(ctx, client, req, known)
		if err == nil && res != nil && faulted(res) == "" && !worthRetrying(res.StatusCode, nil) {
			remember(ctx, routes, domain, known, log)
			return res, nil
		}
		if res != nil {
			res.Body.Close()
		}
		log.Debug("the relay that worked last time did not; starting from the top",
			"url", req.URL.String(), "relay", known.Name(), "status", status(res), "error", err)
		forget(ctx, routes, domain, log)
	}

	res, err := client.Do(req)
	// Whether this refusal is one to learn from, decided before anything is retried and while
	// the direct answer is still in hand.
	worthLearning := !transient(status(res), err)
	if !worthRetrying(status(res), err) {
		// Directly is how it should be reached, so anything learned to the contrary is now
		// wrong. This is the path that notices a block has lifted, and it is only reached
		// because a stale route stopped being tried first.
		forget(ctx, routes, domain, log)
		return res, err
	}

	if len(list()) == 0 {
		return res, err
	}
	log.Debug("the direct request did not get through; trying the relays",
		"url", req.URL.String(), "status", status(res), "error", err, "relays", len(list()))

	for _, proxy := range list() {
		relayed, relayErr := once(ctx, client, req, proxy)
		if relayErr == nil && relayed != nil && faulted(relayed) == "" &&
			!worthRetrying(relayed.StatusCode, nil) {
			// The direct answer is no longer the one being returned, so its body is closed
			// here rather than left for a caller that will never see it.
			if res != nil {
				res.Body.Close()
			}
			log.Info("reached a publisher through a relay",
				"url", req.URL.String(), "relay", proxy.Name(), "status", relayed.StatusCode,
				"remembering", worthLearning)
			if worthLearning {
				remember(ctx, routes, domain, proxy, log)
			}
			return relayed, nil
		}
		if relayed != nil {
			relayed.Body.Close()
		}
		log.Debug("a relay did not get through either",
			"url", req.URL.String(), "relay", proxy.Name(),
			"status", status(relayed), "error", relayErr, "relay_fault", faulted(relayed))
	}
	return res, err
}

// remembered is the relay that reached this domain last time.
//
// Nil for three reasons, and they are the same answer: nothing was ever learned, it names a
// relay the administrator has since deleted or switched off, or the list would not read. In
// every case the ladder starts from the top and learns again.
//
// There is no expiry. A route is what this instance learned about reaching a publisher, and
// that does not go stale on a timer — it goes stale when the relay stops working, which the
// caller notices by the relay failing and drops the route there. An operator who wants one
// forgotten anyway switches the relay off and on again: the route names an id that is briefly
// not in the list, so the next fetch works it out from the top.
func remembered(ctx context.Context, routes Routes, list func() []*store.Proxy,
	domain string, log *slog.Logger) *store.Proxy {

	if routes == nil || domain == "" {
		return nil
	}
	id, _, err := routes.ProxyRoute(ctx, domain)
	if err != nil {
		// Treated as "nothing learned" rather than as a failure. The whole point of this is
		// to save a request, and refusing to fetch because a cache would not read is a much
		// worse outcome than making the request.
		log.Error("could not read the remembered relay; starting from the top",
			"domain", domain, "error", err)
		return nil
	}
	if id == "" {
		return nil
	}
	for _, proxy := range list() {
		if proxy.ID == id {
			return proxy
		}
	}
	log.Debug("the relay that reached this publisher is no longer configured",
		"domain", domain, "relay", id)
	return nil
}

func remember(ctx context.Context, routes Routes, domain string, proxy *store.Proxy, log *slog.Logger) {
	if routes == nil || domain == "" {
		return
	}
	if err := routes.RememberProxyRoute(ctx, domain, proxy.ID); err != nil {
		// Worth a line and nothing more: the request succeeded, and the only cost of not
		// having written this down is doing the work again next time.
		log.Warn("could not remember which relay reached a publisher",
			"domain", domain, "relay", proxy.Name(), "error", err)
	}
}

func forget(ctx context.Context, routes Routes, domain string, log *slog.Logger) {
	if routes == nil || domain == "" {
		return
	}
	if err := routes.ForgetProxyRoute(ctx, domain); err != nil {
		log.Warn("could not forget a relay route", "domain", domain, "error", err)
	}
}

// once sends one request through one relay, whichever way that kind is spoken to.
//
// The two kinds diverge here and nowhere else. proxio is a rewritten URL over the ordinary
// client; SOCKS5 is the same URL over a client that dials somewhere else first. Everything
// above this function — the order, the remembering, what counts as a failure — is the same for
// both, which is what the kind was for.
func once(ctx context.Context, client *http.Client, req *http.Request,
	proxy *store.Proxy) (*http.Response, error) {

	send, sender, err := dial(client, req, proxy)
	if err != nil {
		return nil, err
	}

	// A fresh body for every attempt. A request read once is a request with nothing left to
	// send. Every request made here is a GET with no body — but the ladder would silently send
	// an empty one the day that stops being true, so it is rebuilt rather than assumed.
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		send.Body = body
	}

	res, err := sender.Do(send.WithContext(ctx))
	if err != nil {
		// Never the transport's own error, which is a *url.Error carrying the whole address
		// it dialled — and for proxio that address ends `?url=…&token=…`. Logged as it comes,
		// every failed relay attempt writes the credential into the log file.
		//
		// What is worth keeping is which relay and what went wrong, so that is what is said.
		return nil, fmt.Errorf("%s could not be reached: %w", proxy.Name(), scrub(err))
	}
	if res != nil {
		// Point the response back at the publisher rather than at the relay.
		//
		// Callers read res.Request.URL to learn where they ended up, and use it as the base
		// for resolving whatever the document says relative to itself. Left as proxio's URL
		// that is wrong twice over: every relative link in the feed would resolve against the
		// relay, and the address is `…/proxy?url=…&token=…`, so the credential would be
		// written into the feed's stored FinalURL and into the log line beside it.
		//
		// The address asked for, not the address arrived at: redirects were followed inside
		// the relay and it does not say where they went. Naming the target we requested is the
		// true thing that can be said, and it is what a fetch reports anyway whenever a
		// publisher does not move. A SOCKS5 response already points at the publisher, so this
		// is a no-op for that kind rather than a special case.
		res.Request = req
	}
	return res, nil
}

// dial is the request to send and the client to send it with, for one relay.
func dial(base *http.Client, req *http.Request, p *store.Proxy) (*http.Request, *http.Client, error) {
	switch p.Kind {
	case store.ProxioKind:
		sent, err := through(req, p)
		return sent, base, err
	case store.SocksKind:
		// The request is untouched; what changes is underneath it. Clients are kept per
		// endpoint so a connection can be reused — see socksClients.
		client, err := socksClients.get(p, base.Timeout)
		return req, client, err
	default:
		return nil, nil, fmt.Errorf("%s is a %q proxy, which this does not know how to dial",
			p.Name(), p.Kind)
	}
}

// available is the relays to try, or none when nothing is configured or the list will not load.
//
// A list that cannot be read is logged and treated as empty rather than failed, because the
// alternative is that a database hiccup stops an instance fetching anything at all — and the
// direct request, which is what nearly every feed answers, has already been made by then.
func available(ctx context.Context, relays Relays, log *slog.Logger) []*store.Proxy {
	if relays == nil {
		return nil
	}
	list, err := relays(ctx)
	if err != nil {
		log.Error("could not read the proxy list; fetching directly only", "error", err)
		return nil
	}
	return list
}

// do is [relay] with the fetcher's own client, relays, routes and log.
func (f *Fetcher) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	return relay(ctx, f.client, req, f.Proxies, f.Routes, f.log())
}

func (f *Fetcher) log() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.New(slog.DiscardHandler)
}

// scrub takes the dialled address out of a transport error, keeping what it said.
//
// net/http wraps every failure in a *url.Error whose Error() prints the URL — which for a relay
// is the one place a token is written down outside the database. Unwrapping to the cause keeps
// "connection refused" and "context deadline exceeded" and loses the address, which the caller
// names itself anyway.
func scrub(err error) error {
	var wrapped *url.Error
	if errors.As(err, &wrapped) && wrapped.Err != nil {
		return wrapped.Err
	}
	return err
}

// status is a response's code, or zero when there is no response.
func status(res *http.Response) int {
	if res == nil {
		return 0
	}
	return res.StatusCode
}

// faulted is what a relay said about its own failure, when the failure was its own.
func faulted(res *http.Response) string {
	if res == nil {
		return ""
	}
	return res.Header.Get(proxyError)
}

// TestProxy asks one relay to fetch one address, and reports what the target answered.
//
// Exported for the administrator's Test button, and it goes through the relay rather than
// merely opening a connection to it. The two fail differently: a relay will accept a connection
// and then refuse a stale token, and somebody who saw "reachable" would find that out later
// from a feed that had quietly stopped updating.
//
// The relay's own failures are told apart from the target's by [proxyError], so "your token is
// wrong" and "that site is down" are different answers rather than one shrug.
func TestProxy(ctx context.Context, proxy *store.Proxy, target string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, fmt.Errorf("%s is not a URL that could be fetched: %w", target, err)
	}

	// The same ladder rung a real fetch would use, so what is being tested is the thing that
	// will be used rather than a second implementation of it that could drift.
	res, err := once(ctx, &http.Client{Timeout: requestTimeout}, req, proxy)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if fault := faulted(res); fault != "" {
		return res.StatusCode, fmt.Errorf("%s answered %s and said the fault was its own: %s",
			proxy.Name(), res.Status, fault)
	}
	return res.StatusCode, nil
}
