package api

import (
	"io"
	"net/http"
	"testing"

	"bystander/internal/store"
)

func TestSMTPStartsUnconfiguredWithUsefulDefaults(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var body smtpBody
	h.expect(h.do(http.MethodGet, "/api/admin/smtp", nil), http.StatusOK, &body)
	if body.Configured {
		t.Fatal("a fresh instance claims a relay is set up")
	}
	// An empty form still knows what most relays want, so nobody has to look it up.
	if body.Port != 587 || body.TLS != "starttls" {
		t.Errorf("unhelpful defaults: port %d, tls %q", body.Port, body.TLS)
	}
}

func TestSMTPRoundTripsWithoutEverReturningThePassword(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	relay := map[string]any{
		"host": "smtp.example.com", "port": 465, "tls": "implicit",
		"username": "operator", "password": "hunter2",
		"from_address": "paper@example.com", "sender_name": "Rundschau",
	}
	var saved smtpBody
	h.expect(h.do(http.MethodPut, "/api/admin/smtp", relay), http.StatusOK, &saved)
	if !saved.Configured || saved.Host != "smtp.example.com" || saved.Port != 465 {
		t.Fatalf("saved = %+v", saved)
	}

	// The password is write-only. Whatever the page shows, it cannot show that.
	res := h.do(http.MethodGet, "/api/admin/smtp", nil)
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(body), "hunter2") || contains(string(body), "password") {
		t.Errorf("the configuration handed the password back:\n%s", body)
	}

	// It is still there to send with, even though nothing can read it out over HTTP.
	settings, err := h.store.SMTPSettings(t.Context())
	if err != nil || settings == nil {
		t.Fatalf("SMTPSettings() = %v, %v", settings, err)
	}
	if settings.Password != "hunter2" {
		t.Errorf("password = %q", settings.Password)
	}
}

func TestSMTPKeepsThePasswordWhenTheFormOmitsIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	base := map[string]any{
		"host": "smtp.example.com", "port": 587, "tls": "starttls",
		"username": "operator", "password": "hunter2", "from_address": "paper@example.com",
	}
	h.expect(h.do(http.MethodPut, "/api/admin/smtp", base), http.StatusOK, nil)

	// Correcting a port must not mean retyping a password the page never showed.
	base["port"] = 2525
	base["password"] = ""
	h.expect(h.do(http.MethodPut, "/api/admin/smtp", base), http.StatusOK, nil)

	settings, err := h.store.SMTPSettings(t.Context())
	if err != nil || settings == nil {
		t.Fatalf("SMTPSettings() = %v, %v", settings, err)
	}
	if settings.Port != 2525 {
		t.Errorf("port = %d, want the corrected one", settings.Port)
	}
	if settings.Password != "hunter2" {
		t.Errorf("password = %q, want the one already stored", settings.Password)
	}
}

func TestSMTPNeedsAPasswordTheFirstTime(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	// There is nothing to fall back to, so a blank field is a mistake rather than "leave
	// it as it was".
	h.expect(h.do(http.MethodPut, "/api/admin/smtp", map[string]any{
		"host": "smtp.example.com", "port": 587, "tls": "starttls",
		"username": "operator", "password": "", "from_address": "paper@example.com",
	}), http.StatusBadRequest, nil)
}

func TestSMTPRefusesHalfAConfiguration(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	for _, bad := range []map[string]any{
		{"host": "", "port": 587, "tls": "starttls", "username": "op", "password": "p", "from_address": "a@b.com"},
		{"host": "h", "port": 0, "tls": "starttls", "username": "op", "password": "p", "from_address": "a@b.com"},
		{"host": "h", "port": 587, "tls": "none", "username": "op", "password": "p", "from_address": "a@b.com"},
		{"host": "h", "port": 587, "tls": "starttls", "username": "", "password": "p", "from_address": "a@b.com"},
		{"host": "h", "port": 587, "tls": "starttls", "username": "op", "password": "p", "from_address": "not an address"},
	} {
		res := h.do(http.MethodPut, "/api/admin/smtp", bad)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%v was accepted with %d", bad, res.StatusCode)
		}
		res.Body.Close()
	}
}

func TestSMTPCanBeForgotten(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	h.expect(h.do(http.MethodPut, "/api/admin/smtp", map[string]any{
		"host": "smtp.example.com", "port": 587, "tls": "starttls",
		"username": "operator", "password": "hunter2", "from_address": "paper@example.com",
	}), http.StatusOK, nil)
	h.expect(h.do(http.MethodDelete, "/api/admin/smtp", nil), http.StatusNoContent, nil)

	var body smtpBody
	h.expect(h.do(http.MethodGet, "/api/admin/smtp", nil), http.StatusOK, &body)
	if body.Configured {
		t.Error("the relay outlived being deleted")
	}
	// And sending is refused rather than attempted against nothing.
	h.expect(h.do(http.MethodPost, "/api/admin/smtp/test",
		map[string]string{"to": "reader@example.org"}), http.StatusConflict, nil)
}

func TestSMTPIsAdminsOnly(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "reader")

	// A relay's hostname and username are infrastructure, and a reader has no business
	// with either of them, let alone with replacing them.
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/admin/smtp", nil},
		{http.MethodPut, "/api/admin/smtp", map[string]any{"host": "h"}},
		{http.MethodDelete, "/api/admin/smtp", nil},
		{http.MethodPost, "/api/admin/smtp/test", map[string]string{"to": "a@b.com"}},
	} {
		res := h.do(call.method, call.path, call.body)
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", call.method, call.path, res.StatusCode)
		}
		res.Body.Close()
	}
}
