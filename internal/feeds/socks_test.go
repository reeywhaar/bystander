package feeds

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// socksServer is a SOCKS5 endpoint, enough of one to be dialled through for real.
//
// A real handshake rather than a stand-in, because the thing worth testing is that a request
// goes out over a connection somebody else made — and a stub that returned canned bytes would
// pass whether or not any of that happened. It speaks only what is needed: the greeting, the
// two auth methods, and CONNECT.
func socksServer(t *testing.T, user, pass string) (addr string, dialled *[]string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	var seen []string
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSocks(conn, user, pass, &seen)
		}
	}()
	return listener.Addr().String(), &seen
}

func serveSocks(client net.Conn, user, pass string, seen *[]string) {
	defer client.Close()

	// Greeting: version, how many methods, then the methods.
	head := make([]byte, 2)
	if _, err := io.ReadFull(client, head); err != nil || head[0] != 5 {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}

	wantAuth := user != "" || pass != ""
	method := byte(0x00) // none required
	if wantAuth {
		method = 0x02 // username and password
	}
	client.Write([]byte{5, method})

	if wantAuth {
		// Version, name length, name, password length, password.
		v := make([]byte, 2)
		if _, err := io.ReadFull(client, v); err != nil {
			return
		}
		name := make([]byte, v[1])
		io.ReadFull(client, name)
		pl := make([]byte, 1)
		io.ReadFull(client, pl)
		secret := make([]byte, pl[0])
		io.ReadFull(client, secret)

		if string(name) != user || string(secret) != pass {
			client.Write([]byte{1, 0x01}) // refused
			return
		}
		client.Write([]byte{1, 0x00})
	}

	// CONNECT: version, command, reserved, address type.
	req := make([]byte, 4)
	if _, err := io.ReadFull(client, req); err != nil || req[1] != 0x01 {
		return
	}
	var host string
	switch req[3] {
	case 0x01: // IPv4
		b := make([]byte, 4)
		io.ReadFull(client, b)
		host = net.IP(b).String()
	case 0x03: // a name, which is what x/net sends for anything that is not an IP
		l := make([]byte, 1)
		io.ReadFull(client, l)
		b := make([]byte, l[0])
		io.ReadFull(client, b)
		host = string(b)
	default:
		client.Write([]byte{5, 0x08, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	pb := make([]byte, 2)
	io.ReadFull(client, pb)
	port := binary.BigEndian.Uint16(pb)

	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	*seen = append(*seen, target)

	upstream, err := net.DialTimeout("tcp", target, 3*time.Second)
	if err != nil {
		client.Write([]byte{5, 0x05, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	client.Write([]byte{5, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})

	go io.Copy(upstream, client)
	io.Copy(client, upstream)
}

// A publisher that refuses this address is reached by dialling through SOCKS5.
func TestABlockedFeedIsFetchedThroughSocks(t *testing.T) {
	var direct int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both arrive from 127.0.0.1, so the address cannot tell them apart. What can: a
		// SOCKS request never touches the fetcher's own transport — it goes out over the
		// client the cache built — so anything carrying this header came the direct way.
		if r.Header.Get("X-Came-Direct") != "" {
			direct++
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	}))
	defer origin.Close()

	addr, dialled := socksServer(t, "operator", "hunter2")
	socks := &store.Proxy{ID: "px_1", Kind: store.SocksKind,
		URL: "socks5://" + addr, Username: "operator", Token: "hunter2", Enabled: true}

	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(socks)
	// Everything that leaves through the SOCKS client is marked, which is how the one origin
	// tells a dialled request from a direct one.
	f.client.Transport = markingAll{http.DefaultTransport}

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if got.Parsed == nil || got.Parsed.Title != "Blocked Daily" {
		t.Fatalf("SOCKS did not bring back the feed: %+v", got)
	}
	if direct != 1 {
		t.Errorf("the publisher was asked directly %d times, want one before the endpoint", direct)
	}
	if len(*dialled) != 1 {
		t.Fatalf("the endpoint was asked to connect %d times, want 1", len(*dialled))
	}
	if want := strings.TrimPrefix(origin.URL, "http://"); (*dialled)[0] != want {
		t.Errorf("the endpoint was asked to connect to %q, want %q", (*dialled)[0], want)
	}
	// The URL is the publisher's, because SOCKS never rewrote it.
	if got.FinalURL != origin.URL {
		t.Errorf("FinalURL = %q, want %q", got.FinalURL, origin.URL)
	}
}

// markingAll tags every request that goes out over the fetcher's own transport.
//
// Which is exactly the direct ones: a SOCKS request is made by the client the cache built and
// never reaches this RoundTripper at all. That absence is the only reliable difference — both
// connections arrive at the origin from 127.0.0.1.
type markingAll struct{ inner http.RoundTripper }

func (m markingAll) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Came-Direct", "1")
	return m.inner.RoundTrip(req)
}

// A wrong password is the endpoint's refusal, not the publisher's.
func TestSocksWithTheWrongPasswordFallsThrough(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("blocked here"))
	}))
	defer origin.Close()

	addr, dialled := socksServer(t, "operator", "the-right-one")
	socks := &store.Proxy{ID: "px_1", Kind: store.SocksKind,
		URL: "socks5://" + addr, Username: "operator", Token: "the-wrong-one", Enabled: true}

	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(socks)

	got, err := f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())
	if err == nil {
		t.Fatal("Fetch() succeeded though the endpoint refused the password")
	}
	// What is reported is the publisher's own answer, not the endpoint's handshake failure.
	if got == nil || got.Status != http.StatusForbidden {
		t.Fatalf("reported %+v, want the publisher's 403", got)
	}
	if len(*dialled) != 0 {
		t.Errorf("the endpoint connected anyway despite refusing the password: %v", *dialled)
	}
}

// The endpoint's address and password never reach a log line.
func TestASocksPasswordIsNotWrittenAnywhere(t *testing.T) {
	const pass = "a-very-secret-password"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer origin.Close()

	// An endpoint that is not listening, so the failure goes through the transport's error.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	logged := new(strings.Builder)
	f := NewFetcher("http://read.example.com")
	f.Proxies = relayList(&store.Proxy{ID: "px_1", Kind: store.SocksKind,
		URL: "socks5://" + deadAddr, Username: "operator", Token: pass, Enabled: true})
	f.Log = newTestLogger(logged)

	f.Fetch(t.Context(), &store.Feed{CanonicalURL: origin.URL}, time.Now())

	if strings.Contains(logged.String(), pass) {
		t.Errorf("the password was written to the log:\n%s", logged.String())
	}
	if !strings.Contains(logged.String(), "relay") {
		t.Errorf("nothing about the endpoint reached the log:\n%s", logged.String())
	}
}
