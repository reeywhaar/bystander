package feeds

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"bystander/internal/store"
)

// socksClients keeps one client per SOCKS endpoint, so a connection can be reused.
//
// A transport is meant to be long-lived and shared — it is where the connection pool lives —
// and building one per request means a fresh TCP handshake, a fresh SOCKS handshake and a fresh
// TLS handshake for every picture measured out of one batch.
//
// Keyed by what the connection actually depends on rather than by the row's id, so a relay
// whose password is corrected gets a new client rather than going on using the old credential
// out of a cache. An id would be the tempting key and would be wrong exactly when it mattered.
var socksClients = &clientCache{clients: map[string]*http.Client{}}

// maxSocksClients bounds the cache.
//
// It only grows when a relay's address or credential changes, so on any real instance it holds
// as many entries as there are relays. The bound is for the pathological case — something
// rewriting a relay in a loop — and clearing rather than evicting one entry keeps it to a few
// lines: the cost of being wrong is rebuilding a handful of transports.
const maxSocksClients = 32

type clientCache struct {
	mu      sync.Mutex
	clients map[string]*http.Client
}

func (c *clientCache) get(p *store.Proxy, timeout time.Duration) (*http.Client, error) {
	key := fingerprint(p, timeout)

	c.mu.Lock()
	defer c.mu.Unlock()
	if client, ok := c.clients[key]; ok {
		return client, nil
	}

	client, err := socksClient(p, timeout)
	if err != nil {
		return nil, err
	}
	if len(c.clients) >= maxSocksClients {
		for _, old := range c.clients {
			old.CloseIdleConnections()
		}
		clear(c.clients)
	}
	c.clients[key] = client
	return client, nil
}

// fingerprint is everything about a relay that changes what a connection through it would be.
//
// Hashed rather than concatenated because the password is in it, and a map key is the sort of
// thing that ends up in a panic message or a heap dump.
func fingerprint(p *store.Proxy, timeout time.Duration) string {
	sum := sha256.Sum256([]byte(
		string(p.Kind) + "\x00" + p.URL + "\x00" + p.Username + "\x00" + p.Token +
			"\x00" + timeout.String()))
	return hex.EncodeToString(sum[:])
}

// socksClient builds a client whose connections are made through a SOCKS5 endpoint.
//
// The request is untouched: unlike proxio, nothing about the URL changes, so a response coming
// back through one of these already points at the publisher and needs no repair.
func socksClient(p *store.Proxy, timeout time.Duration) (*http.Client, error) {
	target, err := url.Parse(p.URL)
	if err != nil {
		return nil, fmt.Errorf("%s has an address that will not parse: %w", p.Name(), err)
	}

	var auth *proxy.Auth
	if p.Username != "" || p.Token != "" {
		auth = &proxy.Auth{User: p.Username, Password: p.Token}
	}

	// A dialer with its own connect timeout, so a SOCKS endpoint that accepts a connection and
	// then says nothing does not spend the whole request budget before the target is reached.
	base := &net.Dialer{Timeout: socksConnectTimeout, KeepAlive: 30 * time.Second}
	dialer, err := proxy.SOCKS5("tcp", target.Host, auth, base)
	if err != nil {
		return nil, fmt.Errorf("%s could not be set up: %w", p.Name(), err)
	}

	// socks5h and socks5 behave identically here, and that is worth writing down rather than
	// leaving as a coincidence.
	//
	// The two schemes differ over who resolves the name: socks5 looks it up locally and sends
	// an address, socks5h sends the name and lets the endpoint look it up. x/net does the
	// second unconditionally — it only sends an address when the host already parses as an IP,
	// and otherwise sends AddrTypeFQDN — so both schemes get socks5h semantics.
	//
	// Which is the useful one anyway: a publisher blocked by DNS rather than by address is only
	// reached when the endpoint does the lookup. socks5h is accepted as the name people expect
	// for that behaviour, not because it selects it.
	contextual, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("%s produced a dialer with no context support", p.Name())
	}
	dial := contextual.DialContext

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dial,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		// Redirects are followed on this side, so the same bound applies as anywhere else.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}, nil
}

// socksConnectTimeout bounds reaching the endpoint itself, as opposed to the target beyond it.
const socksConnectTimeout = 10 * time.Second
