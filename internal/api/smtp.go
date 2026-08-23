package api

import (
	"net/http"
	"net/mail"
	"strings"

	mailer "bystander/internal/mail"
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

	settings := mailer.Settings{
		Host:        body.Host,
		Port:        body.Port,
		TLS:         mailer.TLS(body.TLS),
		Username:    body.Username,
		Password:    body.Password,
		FromAddress: body.FromAddress,
		SenderName:  body.SenderName,
	}

	if strings.TrimSpace(body.Password) == "" {
		existing, err := s.store.SMTPSettings(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if existing == nil {
			writeError(w, http.StatusBadRequest, "a password is needed the first time")
			return
		}
		settings.Password = existing.Password
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

	settings, err := s.store.SMTPSettings(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if settings == nil {
		writeError(w, http.StatusConflict, "no relay is configured yet")
		return
	}

	if !s.mail.allow(principalOf(r).ID) {
		writeError(w, http.StatusTooManyRequests, "that is a lot of test messages; wait a minute")
		return
	}

	if err := mailer.Send(r.Context(), *settings, mailer.Message{
		To:      to,
		Subject: "bystander can send mail",
		Body: "This is a test message from bystander.\n\n" +
			"If it reached you, the relay is configured correctly and password recovery " +
			"will be able to use it.\n",
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
