package api

import (
	"net/http"
	"testing"
	"time"

	"bystander/internal/store"
)

func TestSessionsListsThisAccountsSignIns(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	var rows []sessionBody
	h.expect(h.do(http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &rows)
	if len(rows) != 1 {
		t.Fatalf("a fresh account has %d sessions, want 1", len(rows))
	}
	if !rows[0].Current {
		t.Error("the only session is not marked as the current one")
	}
	if rows[0].ID == "" {
		t.Error("a session has no id, so nothing can revoke it")
	}
	if rows[0].LastAccess == 0 || rows[0].CreatedAt == 0 || rows[0].ExpiresAt == 0 {
		t.Errorf("a session has an empty clock: %+v", rows[0])
	}
	// Go's client announces itself, which is all this needs to prove the header is read.
	if rows[0].UserAgent == "" {
		t.Error("no user agent was recorded")
	}
	if rows[0].IP == "" {
		t.Error("no address was recorded")
	}

	// A second sign-in is a second row, and only one of the two is this one.
	h.signInElsewhere("ada", harnessPassword)
	h.expect(h.do(http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &rows)
	if len(rows) != 2 {
		t.Fatalf("after signing in elsewhere there are %d sessions, want 2", len(rows))
	}
	current := 0
	for _, row := range rows {
		if row.Current {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d sessions are marked current, want exactly 1", current)
	}
}

func TestSessionsAreOnlyYourOwn(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	bob := h.signInAsSomebodyElse("bob")

	var mine []sessionBody
	h.expect(h.do(http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &mine)
	var theirs []sessionBody
	h.expect(h.doAs(bob, http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &theirs)

	if len(mine) != 1 || len(theirs) != 1 {
		t.Fatalf("two accounts with one session each list %d and %d", len(mine), len(theirs))
	}
	if mine[0].ID == theirs[0].ID {
		t.Fatal("two different sessions share an id")
	}

	// An id belonging to somebody else matches nothing, rather than deleting their
	// session — the scoping is the lookup, not a check bolted on after it.
	h.expect(h.doAs(bob, http.MethodDelete, "/api/account/sessions/"+mine[0].ID, nil),
		http.StatusNotFound, nil)
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)
}

func TestRevokeOtherSessionsKeepsThisOne(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	elsewhere := h.signInElsewhere("ada", harnessPassword)

	h.expect(h.doAs(elsewhere, http.MethodGet, "/api/account", nil), http.StatusOK, nil)

	h.expect(h.do(http.MethodDelete, "/api/account/sessions", nil), http.StatusNoContent, nil)

	// The tab that pressed the button is the one thing that survives it.
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)
	h.expect(h.doAs(elsewhere, http.MethodGet, "/api/account", nil), http.StatusUnauthorized, nil)

	var rows []sessionBody
	h.expect(h.do(http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &rows)
	if len(rows) != 1 || !rows[0].Current {
		t.Fatalf("after signing out everywhere else, sessions are %+v", rows)
	}
}

func TestRevokeOneSession(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	elsewhere := h.signInElsewhere("ada", harnessPassword)

	var rows []sessionBody
	h.expect(h.do(http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &rows)

	other := ""
	for _, row := range rows {
		if !row.Current {
			other = row.ID
		}
	}
	if other == "" {
		t.Fatal("no session other than this one to revoke")
	}

	h.expect(h.do(http.MethodDelete, "/api/account/sessions/"+other, nil), http.StatusNoContent, nil)
	h.expect(h.doAs(elsewhere, http.MethodGet, "/api/account", nil), http.StatusUnauthorized, nil)
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)

	// Gone means gone: the same id a second time is not a session any more.
	h.expect(h.do(http.MethodDelete, "/api/account/sessions/"+other, nil), http.StatusNotFound, nil)
}

// Revoking the session you are reading from is a coherent thing to ask for, and the answer
// is that you are signed out.
func TestRevokeCurrentSessionSignsYouOut(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	var rows []sessionBody
	h.expect(h.do(http.MethodGet, "/api/account/sessions", nil), http.StatusOK, &rows)

	h.expect(h.do(http.MethodDelete, "/api/account/sessions/"+rows[0].ID, nil), http.StatusNoContent, nil)
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusUnauthorized, nil)
}

func TestSessionsRecordWhereARequestCameFrom(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	// httptest listens on the loopback, so the harness is always "behind a proxy" as far
	// as clientIP is concerned — which is what makes the header believable here.
	//
	// Both requests carry them, including the one that reads the list back: every request
	// records where it came from, so a listing sent from somewhere else would honestly
	// report that somewhere else.
	asPhone := func(path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-For", "198.51.100.9")
		req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "+
			"AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
		res, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// Past session.Move, which is the floor on recording that a session has changed
	// address — without it a request seconds after the sign-in is inside the throttle and
	// records nothing, which is the behaviour a real browser wants and a test cannot wait
	// out.
	h.sessions.SetClock(func() time.Time { return time.Now().Add(2 * time.Minute) })

	asPhone("/api/account").Body.Close()

	var rows []sessionBody
	h.expect(asPhone("/api/account/sessions"), http.StatusOK, &rows)
	if rows[0].IP != "198.51.100.9" {
		t.Errorf("IP = %q, want the forwarded address", rows[0].IP)
	}
	if rows[0].Device != "Safari on iPhone" {
		t.Errorf("Device = %q, want %q", rows[0].Device, "Safari on iPhone")
	}
	if rows[0].UserAgent == "" {
		t.Error("the raw user agent was not kept beside the summary")
	}
}

func TestDescribeAgent(t *testing.T) {
	cases := []struct{ ua, want string }{
		{"", ""},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 " +
			"(KHTML, like Gecko) Version/17.0 Safari/605.1.15", "Safari on Mac"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
			"Chrome/120.0.0.0 Safari/537.36", "Chrome on Windows"},
		// Edge says Chrome and Safari too; the specific token has to win.
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
			"Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", "Edge on Windows"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
			"Firefox on Linux"},
		// Android says Linux as well.
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) " +
			"Chrome/120.0.0.0 Mobile Safari/537.36", "Chrome on Android"},
		{"curl/8.4.0", "curl"},
		{"something nobody has ever shipped", ""},
	}
	for _, c := range cases {
		if got := describeAgent(c.ua); got != c.want {
			t.Errorf("describeAgent(%q) = %q, want %q", c.ua, got, c.want)
		}
	}
}
