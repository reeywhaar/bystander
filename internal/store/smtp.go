package store

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"

	"bystander/internal/ids"
	mailer "bystander/internal/mail"
)

// SMTPSummary is the relay as it is safe to show: everything except the password.
type SMTPSummary struct {
	Host        string
	Port        int
	TLS         mailer.TLS
	Username    string
	FromAddress string
	SenderName  string
	UpdatedAt   time.Time
}

// SMTPConfigured reports whether anything is set up at all, without reading the rest.
//
// Its own query because several places only need the yes or no — a page that says whether
// recovery is possible, a handler deciding whether to try — and reading a password to
// answer that would be reading a password for no reason.
func (s *Store) SMTPConfigured(ctx context.Context) (bool, error) {
	var one int
	err := s.main.QueryRowContext(ctx, `SELECT 1 FROM smtp_config WHERE singleton = 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// SMTPSummary is the configuration without the password. Nil when there is none.
func (s *Store) SMTPSummary(ctx context.Context) (*SMTPSummary, error) {
	var (
		out     SMTPSummary
		tls     string
		updated int64
	)
	err := s.main.QueryRowContext(ctx, `
		SELECT host, port, tls, username, from_address, sender_name, updated_at
		  FROM smtp_config WHERE singleton = 1`).
		Scan(&out.Host, &out.Port, &tls, &out.Username, &out.FromAddress, &out.SenderName, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.TLS = mailer.TLS(tls)
	out.UpdatedAt = time.Unix(updated, 0).UTC()
	return &out, nil
}

// SMTPSettings is the whole configuration, password included. Nil when there is none.
//
// Separate from [Store.SMTPSummary] so that showing the relay and using it are different
// calls: a handler that renders one cannot accidentally serialize the other.
func (s *Store) SMTPSettings(ctx context.Context) (*mailer.Settings, error) {
	var (
		out mailer.Settings
		tls string
	)
	err := s.main.QueryRowContext(ctx, `
		SELECT host, port, tls, username, password, from_address, sender_name
		  FROM smtp_config WHERE singleton = 1`).
		Scan(&out.Host, &out.Port, &tls, &out.Username, &out.Password, &out.FromAddress, &out.SenderName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.TLS = mailer.TLS(tls)
	return &out, nil
}

// SetSMTP replaces the relay configuration, or refuses it.
//
// Credentials are required rather than optional. Relays that want none do exist, but
// accepting a blank password here would make "this relay needs no authentication"
// indistinguishable from "somebody left the field empty" — and the second is far likelier.
func (s *Store) SetSMTP(ctx context.Context, in mailer.Settings) error {
	host, err := required("host", in.Host)
	if err != nil {
		return err
	}
	username, err := required("username", in.Username)
	if err != nil {
		return err
	}
	from, err := required("from address", in.FromAddress)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Password) == "" {
		return Invalid("a relay needs a password; remove the whole configuration instead")
	}
	if in.Port < 1 || in.Port > 65535 {
		return Invalid("port must be between 1 and 65535")
	}
	if in.TLS != mailer.StartTLS && in.TLS != mailer.Implicit {
		return Invalid("tls must be starttls or implicit, not %q", in.TLS)
	}
	// Parsed, not pattern-matched, and no further than this. Whether the address can
	// actually send is the relay's answer, and guessing at it here would refuse addresses
	// that work perfectly well.
	parsed, err := mail.ParseAddress(from)
	if err != nil {
		return Invalid("%q is not an address the relay could send as", from)
	}

	_, err = s.main.ExecContext(ctx, `
		INSERT INTO smtp_config
			(id, singleton, host, port, tls, username, password, from_address, sender_name, updated_at)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (singleton) DO UPDATE SET
			id = excluded.id,
			host = excluded.host,
			port = excluded.port,
			tls = excluded.tls,
			username = excluded.username,
			password = excluded.password,
			from_address = excluded.from_address,
			sender_name = excluded.sender_name,
			updated_at = excluded.updated_at`,
		ids.New(ids.SMTP), host, in.Port, string(in.TLS), username, in.Password,
		parsed.Address, strings.TrimSpace(in.SenderName), time.Now().Unix())
	return err
}

// ClearSMTP forgets the relay. Sending afterwards is refused rather than attempted.
func (s *Store) ClearSMTP(ctx context.Context) error {
	_, err := s.main.ExecContext(ctx, `DELETE FROM smtp_config`)
	return err
}

// required trims a field and refuses one left blank.
func required(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", Invalid("%s is required", field)
	}
	return trimmed, nil
}
