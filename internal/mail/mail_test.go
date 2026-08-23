package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestComposeIsAMessageARelayWouldAccept(t *testing.T) {
	raw, err := compose(
		Settings{FromAddress: "paper@example.com", SenderName: "Rundschau"},
		Message{To: "reader@example.org", Subject: "Über die Zeitung", Body: "line one\nline two\n"},
	)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)

	head, body, ok := strings.Cut(text, "\r\n\r\n")
	if !ok {
		t.Fatalf("no blank line between headers and body:\n%s", text)
	}
	for _, want := range []string{
		// Plain ASCII is left legible rather than encoded for no reason.
		"From: \"Rundschau\" <paper@example.com>",
		"To: <reader@example.org>",
		"MIME-Version: 1.0",
		"Content-Transfer-Encoding: quoted-printable",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("headers missing %q:\n%s", want, head)
		}
	}
	// The subject has a non-ASCII character in it, so it must not appear as itself.
	if strings.Contains(head, "Über") {
		t.Errorf("subject went out unencoded:\n%s", head)
	}
	if !strings.Contains(head, "=?utf-8?q?") {
		t.Errorf("subject was not encoded at all:\n%s", head)
	}
	if body != "line one\r\nline two\r\n" {
		t.Errorf("body newlines were not made CRLF: %q", body)
	}
	// Bare LF anywhere is what makes a relay reformat or reject a message.
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Errorf("message contains a bare newline:\n%q", text)
	}
}

func TestComposeDefaultsTheSenderName(t *testing.T) {
	raw, err := compose(Settings{FromAddress: "paper@example.com"}, Message{To: "a@b.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `From: "`+DefaultSenderName+`" <paper@example.com>`) {
		t.Errorf("no default sender name:\n%s", raw)
	}
}

func TestComposeQuotesANameThatNeedsIt(t *testing.T) {
	raw, err := compose(
		Settings{FromAddress: "paper@example.com", SenderName: "Bell, Book & Candle"},
		Message{To: "reader@example.org"},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Unquoted, the comma would split the header into two addresses, and the second one
	// would not be an address at all.
	if !strings.Contains(string(raw), `From: "Bell, Book & Candle" <paper@example.com>`) {
		t.Errorf("comma in the sender name went out unquoted:\n%s", raw)
	}
}

func TestSendOverImplicitTLS(t *testing.T) {
	relay := startRelay(t, implicit)
	err := Send(context.Background(), relay.settings(Implicit), Message{
		To: "reader@example.org", Subject: "hello", Body: "a body",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	got := relay.conversation()
	for _, want := range []string{"MAIL FROM:<paper@example.com>", "RCPT TO:<reader@example.org>", "DATA", "QUIT"} {
		if !strings.Contains(got, want) {
			t.Errorf("relay never saw %q:\n%s", want, got)
		}
	}
	if !strings.Contains(relay.delivered(), "Subject: hello") {
		t.Errorf("message body never arrived:\n%s", relay.delivered())
	}
	if relay.authUser() != "operator" || relay.authPass() != "hunter2" {
		t.Errorf("wrong credentials: %q / %q", relay.authUser(), relay.authPass())
	}
}

func TestSendUpgradesWithStartTLS(t *testing.T) {
	relay := startRelay(t, starttls)
	err := Send(context.Background(), relay.settings(StartTLS), Message{To: "reader@example.org", Body: "hi"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(relay.conversation(), "STARTTLS") {
		t.Errorf("never upgraded:\n%s", relay.conversation())
	}
	// The second EHLO, after the upgrade, is what proves the connection was really
	// renegotiated rather than carried on in the clear.
	if strings.Count(relay.conversation(), "EHLO") < 2 {
		t.Errorf("did not re-introduce itself after upgrading:\n%s", relay.conversation())
	}
}

func TestSendRefusesARelayThatWillNotUpgrade(t *testing.T) {
	relay := startRelay(t, plainOnly)
	err := Send(context.Background(), relay.settings(StartTLS), Message{To: "reader@example.org", Body: "hi"})
	if err == nil {
		t.Fatal("sent the password over an unencrypted connection")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("unhelpful refusal: %v", err)
	}
	if strings.Contains(relay.conversation(), "hunter2") ||
		strings.Contains(relay.conversation(), base64.StdEncoding.EncodeToString([]byte("\x00operator\x00hunter2"))) {
		t.Errorf("credentials went out anyway:\n%s", relay.conversation())
	}
}

func TestSendReportsWhatTheRelaySaid(t *testing.T) {
	relay := startRelay(t, implicit)
	relay.refuse = "550 5.7.1 relaying denied for that sender"

	err := Send(context.Background(), relay.settings(Implicit), Message{To: "reader@example.org", Body: "hi"})
	if err == nil {
		t.Fatal("a refused message looked like a sent one")
	}
	if !strings.Contains(err.Error(), "relaying denied") {
		t.Errorf("the relay's own words were thrown away: %v", err)
	}
}

func TestSendGivesUpWhenTheRelayStopsAnswering(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	// Accepts and then says nothing at all, which is how a relay behind a dropped route
	// behaves and is the case a timeout exists for.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-make(chan struct{})
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(port)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Send(ctx, Settings{
			Host: host, Port: n, TLS: Implicit,
			Username: "operator", Password: "hunter2", FromAddress: "paper@example.com",
		}, Message{To: "reader@example.org", Body: "hi"})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a silent relay looked like a successful send")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send hung past the context deadline")
	}
}

// --- a relay to send to -----------------------------------------------------------

type mode int

const (
	implicit mode = iota
	starttls
	plainOnly // offers no STARTTLS, to check we refuse rather than carry on
)

type relay struct {
	host   string
	port   int
	certs  *x509.CertPool
	refuse string // when set, what to answer MAIL FROM with

	mu    sync.Mutex
	log   strings.Builder
	data  strings.Builder
	user  string
	pass  string
	ready chan struct{}
}

func (r *relay) settings(t TLS) Settings {
	return Settings{
		Host: r.host, Port: r.port, TLS: t,
		Username: "operator", Password: "hunter2",
		FromAddress: "paper@example.com", SenderName: "Rundschau",
	}
}

func (r *relay) conversation() string { r.mu.Lock(); defer r.mu.Unlock(); return r.log.String() }
func (r *relay) delivered() string    { r.mu.Lock(); defer r.mu.Unlock(); return r.data.String() }
func (r *relay) authUser() string     { r.mu.Lock(); defer r.mu.Unlock(); return r.user }
func (r *relay) authPass() string     { r.mu.Lock(); defer r.mu.Unlock(); return r.pass }

func startRelay(t *testing.T, m mode) *relay {
	t.Helper()
	cert, pool := selfSigned(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(port)
	r := &relay{host: host, port: n, certs: pool}

	// The client verifies against the system store unless told otherwise, and this
	// certificate is one only this test has any reason to trust.
	old := rootCAs
	rootCAs = pool
	t.Cleanup(func() { rootCAs = old })

	conf := &tls.Config{Certificates: []tls.Certificate{cert}}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if m == implicit {
			conn = tls.Server(conn, conf)
		}
		r.serve(conn, conf, m)
	}()
	return r
}

func (r *relay) serve(conn net.Conn, conf *tls.Config, m mode) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	say := func(format string, args ...any) {
		fmt.Fprintf(conn, format+"\r\n", args...)
	}
	say("220 relay.test ESMTP")

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		r.mu.Lock()
		r.log.WriteString(line + "\n")
		r.mu.Unlock()

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			// Whether STARTTLS is on offer changes once it has been used: advertising it
			// again to an already-encrypted client is how a real relay confuses one.
			_, encrypted := conn.(*tls.Conn)
			if m == starttls && !encrypted {
				say("250-relay.test")
				say("250-STARTTLS")
				say("250 8BITMIME")
				continue
			}
			if m == plainOnly {
				say("250-relay.test")
				say("250 8BITMIME")
				continue
			}
			say("250-relay.test")
			say("250-AUTH PLAIN LOGIN")
			say("250 8BITMIME")
		case upper == "STARTTLS":
			say("220 go ahead")
			tconn := tls.Server(conn, conf)
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			br = bufio.NewReader(conn)
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			r.mu.Lock()
			if len(parts) == 3 {
				r.user, r.pass = parts[1], parts[2]
			}
			r.mu.Unlock()
			say("235 authenticated")
		case strings.HasPrefix(upper, "MAIL FROM"):
			if r.refuse != "" {
				say("%s", r.refuse)
				continue
			}
			say("250 ok")
		case strings.HasPrefix(upper, "RCPT TO"):
			say("250 ok")
		case upper == "DATA":
			say("354 go ahead")
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				r.mu.Lock()
				r.data.WriteString(l)
				r.mu.Unlock()
			}
			say("250 queued")
		case upper == "QUIT":
			say("221 bye")
			return
		default:
			say("500 unrecognised")
		}
	}
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}
