// Package mail speaks SMTP to whichever relay an operator has configured.
//
// Split from the store on purpose: the store decides what the relay *is* and holds its
// password, and this is the half that opens a socket. Nothing here touches the database.
//
// Nothing is queued, either. A message goes out while the request that asked for it is
// still open, so the caller learns whether the relay accepted it. That is the entire point
// of a test send, and it is what any later caller wants too — a password-reset mail that
// failed silently is worse than one that failed loudly.
package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base32"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"
)

// TLS is how the connection is protected. There is no third option on purpose.
type TLS string

const (
	// StartTLS upgrades a plain connection, which is what port 587 expects.
	StartTLS TLS = "starttls"
	// Implicit is TLS from the first byte, which is what port 465 expects.
	Implicit TLS = "implicit"
)

// DefaultSenderName is what recipients see when none is configured.
//
// Defaulted here rather than stored, because a default written into the row is a default
// nobody can tell apart from a deliberate choice.
const DefaultSenderName = "bystander"

// Timeout caps the whole conversation — connect, greet, authenticate, send, quit.
//
// A relay that has stopped answering must not hold a request open until the browser gives
// up first, because then the operator learns nothing about why.
const Timeout = 30 * time.Second

// rootCAs is nil, which means the system trust store — the right answer everywhere except
// a test, which has to trust a relay it started itself and whose certificate nothing else
// has any reason to accept.
var rootCAs *x509.CertPool

// Settings are the relay as an operator configured it. Carries the password.
type Settings struct {
	Host        string
	Port        int
	TLS         TLS
	Username    string
	Password    string
	FromAddress string
	SenderName  string
}

// Message is one mail, before any of it is encoded.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Send delivers one message, and reports what the relay said if it refused.
//
// The relay's own words are passed back rather than replaced with "sending failed". An
// operator setting this up needs to know whether the host was wrong, the credentials were
// rejected, or the certificate did not verify, and those are three different afternoons.
func Send(ctx context.Context, s Settings, m Message) error {
	raw, err := compose(s, m)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	c, err := dial(ctx, s)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := authenticate(c, s); err != nil {
		return err
	}
	if err := c.Mail(s.FromAddress); err != nil {
		return fmt.Errorf("the relay refused the sender %s: %w", s.FromAddress, err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("the relay refused the recipient %s: %w", m.To, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("the relay refused the message: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("the relay refused the message: %w", err)
	}
	// Closing is what sends it: the final dot goes out here, and this is the error that
	// says whether it was accepted. Deferring the close would discard exactly that.
	if err := w.Close(); err != nil {
		return fmt.Errorf("the relay refused the message: %w", err)
	}
	return c.Quit()
}

// dial opens the connection and gets TLS around it, one way or the other.
//
// Built per send rather than pooled. This runs when somebody presses a button or asks for
// a reset, not in a loop, and a connection held open against a relay that may rotate its
// certificate is a connection that fails at the least convenient moment.
func dial(ctx context.Context, s Settings) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	// The name to verify the certificate against is the configured host, never anything
	// the relay says about itself later.
	conf := &tls.Config{ServerName: s.Host, RootCAs: rootCAs}
	d := &net.Dialer{}

	if s.TLS == Implicit {
		conn, err := (&tls.Dialer{NetDialer: d, Config: conf}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("could not reach the relay at %s: %w", addr, err)
		}
		c, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("the relay at %s did not greet us: %w", addr, err)
		}
		return c, nil
	}

	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("could not reach the relay at %s: %w", addr, err)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the relay at %s did not greet us: %w", addr, err)
	}
	// Required, not attempted. A relay that will not upgrade is a relay that would carry
	// this password across the network in the clear, and carrying on anyway would hide
	// that from the only person who could fix it.
	if ok, _ := c.Extension("STARTTLS"); !ok {
		c.Close()
		return nil, fmt.Errorf("the relay at %s does not offer STARTTLS; try implicit TLS, usually on port 465", addr)
	}
	if err := c.StartTLS(conf); err != nil {
		c.Close()
		return nil, fmt.Errorf("could not start TLS with %s: %w", addr, err)
	}
	return c, nil
}

// authenticate signs in with whichever mechanism the relay actually offers.
//
// PLAIN is the one everything supports and the only one Go ships. LOGIN is the same
// credentials in a sillier shape, and enough relays offer nothing else — Office 365 among
// them — that refusing to speak it would mean refusing to send at all.
func authenticate(c *smtp.Client, s Settings) error {
	ok, mechanisms := c.Extension("AUTH")
	if !ok {
		// The relay wants no credentials. Configuration requires them, so this is a
		// mismatch worth naming rather than a connection to carry on with.
		return fmt.Errorf("the relay at %s does not accept credentials", s.Host)
	}

	var auth smtp.Auth
	switch {
	case strings.Contains(mechanisms, "PLAIN"):
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	case strings.Contains(mechanisms, "LOGIN"):
		auth = &loginAuth{username: s.Username, password: s.Password, host: s.Host}
	default:
		return fmt.Errorf("the relay at %s offers only %s, and neither is one we can speak", s.Host, mechanisms)
	}

	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("the relay rejected the credentials: %w", err)
	}
	return nil
}

// loginAuth is AUTH LOGIN: the username and then the password, each base64, each in answer
// to a prompt whose wording is not standardised and therefore not checked.
type loginAuth struct {
	username, password, host string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// The same refusal PlainAuth makes, for the same reason: these credentials are worth
	// as much as the mailbox, and handing them to an unverified peer loses them.
	if !server.TLS {
		return "", nil, fmt.Errorf("refusing to send credentials over an unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, fmt.Errorf("refusing to send credentials to %s, which is not %s", server.Name, a.host)
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	// Prompts are conventionally "Username:" and "Password:", but relays word them
	// differently and some send them base64 in a different case. Answering in order is
	// what the mechanism actually specifies.
	switch {
	case strings.Contains(strings.ToLower(string(fromServer)), "user"):
		return []byte(a.username), nil
	case strings.Contains(strings.ToLower(string(fromServer)), "pass"):
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("the relay asked something unexpected during login: %q", fromServer)
}

// compose renders the message as RFC 5322 bytes, CRLF and all.
//
// Written out rather than assembled with a library: it is a dozen headers and a
// quoted-printable body, and a dependency for that would be a dependency to audit.
func compose(s Settings, m Message) ([]byte, error) {
	name := s.SenderName
	if strings.TrimSpace(name) == "" {
		name = DefaultSenderName
	}

	var b strings.Builder
	// net/mail rather than string concatenation. A display name needs quoting when it
	// holds a comma, an apostrophe or a full stop, and encoding when it holds anything
	// outside ASCII — and getting either wrong produces a header that parses as a
	// different address than the one meant.
	from := netmail.Address{Name: name, Address: s.FromAddress}
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + (&netmail.Address{Address: m.To}).String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + messageID(s.FromAddress) + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")

	// Quoted-printable rather than raw 8-bit: it keeps every line inside the 998
	// characters a message is allowed, whatever the body turned out to be.
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(normalizeNewlines(m.Body))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// normalizeNewlines makes every line ending CRLF, whatever it was.
//
// Bodies are written as Go string literals with bare \n, and a message with mixed endings
// is one some relays reformat and others reject.
func normalizeNewlines(body string) string {
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
}

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// messageID is unique enough to identify this message in a relay's logs and says nothing
// else. The domain half comes from the sender, because a Message-ID whose domain is not
// one we send from is a small spam signal.
func messageID(from string) string {
	var buf [16]byte
	rand.Read(buf[:])
	domain := "localhost"
	if _, after, ok := strings.Cut(from, "@"); ok && after != "" {
		domain = after
	}
	return strings.ToLower(crockford.EncodeToString(buf[:])) + "@" + domain
}
