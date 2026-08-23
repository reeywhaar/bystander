package api

import (
	"net/http"
	"net/mail"
	"strings"

	mailer "bystander/internal/mail"
	"bystander/internal/store"
)

// smtpBody is the relay as an administrator sees it. No password: it is write-only, so a
// page that renders the configuration cannot hand it back out again.
type smtpBody struct {
	Configured  bool   `json:"configured"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	TLS         string `json:"tls"`
	Username    string `json:"username"`
	FromAddress string `json:"from_address"`
	SenderName  string `json:"sender_name"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (s *Server) getSMTP(w http.ResponseWriter, r *http.Request) {
	summary, err := s.store.SMTPSummary(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// An unconfigured relay is an empty form rather than a 404: there is nothing missing,
	// it just has not been set up, and the form's defaults belong in one place.
	if summary == nil {
		writeJSON(w, http.StatusOK, smtpBody{
			Port: 587,
			TLS:  string(mailer.StartTLS),
		})
		return
	}
	writeJSON(w, http.StatusOK, smtpBody{
		Configured:  true,
		Host:        summary.Host,
		Port:        summary.Port,
		TLS:         string(summary.TLS),
		Username:    summary.Username,
		FromAddress: summary.FromAddress,
		SenderName:  summary.SenderName,
		UpdatedAt:   summary.UpdatedAt.Unix(),
	})
}

// settings is the request as the mail package wants it, before any of it is checked.
func (b putSMTPRequest) settings() mailer.Settings {
	return mailer.Settings{
		Host:        b.Host,
		Port:        b.Port,
		TLS:         mailer.TLS(b.TLS),
		Username:    b.Username,
		Password:    b.Password,
		FromAddress: b.FromAddress,
		SenderName:  b.SenderName,
	}
}

// keepPassword fills in a password left empty from the one already stored.
//
// The password is never sent to the browser, so requiring it on every write would mean
// retyping a secret nobody can see in order to correct a port number. Empty therefore means
// "the one already there" — except the first time, when there is nothing to mean.
//
// Reports false when it has already written a response.
func (s *Server) keepPassword(w http.ResponseWriter, r *http.Request, in mailer.Settings) (mailer.Settings, bool) {
	if strings.TrimSpace(in.Password) != "" {
		return in, true
	}
	existing, err := s.store.SMTPSettings(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return in, false
	}
	if existing == nil {
		writeError(w, http.StatusBadRequest, "a password is needed the first time")
		return in, false
	}
	in.Password = existing.Password
	return in, true
}

type putSMTPRequest struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	TLS         string `json:"tls"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromAddress string `json:"from_address"`
	SenderName  string `json:"sender_name"`
}

// putSMTP replaces the whole configuration.
//
// PUT rather than PATCH because these fields only make sense together: a host changed
// without the credentials that go with it is a relay that cannot be reached, and letting
// somebody save half of one is letting them break sending in a way the form hides.
//
// The one exception is the password, which an empty value leaves alone. It is never sent
// back to the browser, so requiring it on every save would mean retyping it to correct a
// port number.
func (s *Server) putSMTP(w http.ResponseWriter, r *http.Request) {
	var body putSMTPRequest
	if !decode(w, r, &body) {
		return
	}

	settings := body.settings()

	settings, ok := s.keepPassword(w, r, settings)
	if !ok {
		return
	}

	if err := s.store.SetSMTP(r.Context(), settings); err != nil {
		s.fail(w, r, err)
		return
	}
	s.getSMTP(w, r)
}

func (s *Server) deleteSMTP(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearSMTP(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type testSMTPRequest struct {
	To string `json:"to"`
	// Relay, when given, is tried instead of the stored one and nothing is written.
	//
	// This is what makes it possible to find out whether a relay works before committing
	// to it. Without it the only way to test a password is to save it, and by then the
	// working configuration it replaced is gone.
	Relay *putSMTPRequest `json:"relay"`
}

// testSMTP sends one real message, and says what the relay said.
//
// It sends rather than merely connecting, because the two fail differently: a relay will
// happily accept a login and then refuse the From address, and an operator who saw
// "connected" would find that out later, from somebody who could not get in.
func (s *Server) testSMTP(w http.ResponseWriter, r *http.Request) {
	var body testSMTPRequest
	if !decode(w, r, &body) {
		return
	}
	to := strings.TrimSpace(body.To)
	if to == "" {
		writeError(w, http.StatusBadRequest, "an address to send to is required")
		return
	}
	if _, err := mail.ParseAddress(to); err != nil {
		writeError(w, http.StatusBadRequest, "that is not an address we could send to")
		return
	}

	settings, ok := s.relayToTry(w, r, body.Relay)
	if !ok {
		return
	}

	if !s.mail.allow(principalOf(r).ID) {
		writeError(w, http.StatusTooManyRequests, "that is a lot of test messages; wait a minute")
		return
	}

	if err := mailer.Send(r.Context(), settings, mailer.Message{
		To:      to,
		Subject: "bystander can send mail",
		// Deliberately says nothing about the relay being saved: this same message goes
		// out for settings that have only been typed, and telling somebody their relay is
		// configured when they have not pressed Save yet would be a small lie with a
		// large consequence.
		Body: "This is a test message from bystander.\n\n" +
			"If it reached you, the relay accepted it and can be used to send mail.\n",
	}); err != nil {
		// 502 rather than 500. Everything on this side worked and something upstream did
		// not, and a 500 would send an operator looking through the wrong logs. The
		// relay's own words go with it, because "the host was wrong", "the credentials
		// were rejected" and "the certificate did not verify" are three different
		// afternoons.
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// relayToTry picks between the settings in the request and the ones already stored.
//
// Typed settings are checked exactly as a save would check them, so a test cannot pass
// against a configuration the database would then refuse.
//
// Reports false when it has already written a response.
func (s *Server) relayToTry(w http.ResponseWriter, r *http.Request, draft *putSMTPRequest) (mailer.Settings, bool) {
	if draft == nil {
		stored, err := s.store.SMTPSettings(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return mailer.Settings{}, false
		}
		if stored == nil {
			writeError(w, http.StatusConflict, "no relay is configured yet")
			return mailer.Settings{}, false
		}
		return *stored, true
	}

	settings, ok := s.keepPassword(w, r, draft.settings())
	if !ok {
		return mailer.Settings{}, false
	}
	settings, err := store.ValidateSMTP(settings)
	if err != nil {
		s.fail(w, r, err)
		return mailer.Settings{}, false
	}
	return settings, true
}
