package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"bystander/internal/store"
)

type proxyList struct {
	Proxies []proxyBody `json:"proxies"`
}

func TestProxiesStartEmptyAndRoundTripWithoutReturningTheToken(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var list proxyList
	h.expect(h.do(http.MethodGet, "/api/admin/proxies", nil), http.StatusOK, &list)
	if len(list.Proxies) != 0 {
		t.Fatalf("a fresh instance already has %d relays", len(list.Proxies))
	}

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "label": "Frankfurt",
		"url": "http://proxio.internal:80", "token": "hunter2",
	}), http.StatusCreated, &added)
	if added.Label != "Frankfurt" || added.URL != "http://proxio.internal:80" {
		t.Fatalf("added = %+v", added)
	}
	if !added.Enabled {
		t.Error("a relay somebody just added arrived switched off")
	}
	if !added.HasToken {
		t.Error("the relay does not admit to having a token")
	}

	// The token is write-only. Whatever the page shows, it cannot show that.
	res := h.do(http.MethodGet, "/api/admin/proxies", nil)
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(body), "hunter2") || contains(string(body), "\"token\"") {
		t.Errorf("the list handed the token back:\n%s", body)
	}

	// It is still there to relay with, even though nothing can read it out over HTTP.
	stored, err := h.store.Proxies(t.Context())
	if err != nil || len(stored) != 1 {
		t.Fatalf("Proxies() = %v, %v", stored, err)
	}
	if stored[0].Token != "hunter2" {
		t.Errorf("token = %q", stored[0].Token)
	}
}

// A label can be corrected without retyping a credential nobody can see.
func TestEditingARelayKeepsTheTokenItAlreadyHas(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "url": "https://proxio.example.com", "token": "keep-me",
	}), http.StatusCreated, &added)

	var edited proxyBody
	h.expect(h.do(http.MethodPut, "/api/admin/proxies/"+added.ID, map[string]any{
		"kind": "proxio", "label": "Renamed", "url": "https://proxio.example.com", "token": "",
	}), http.StatusOK, &edited)
	if edited.Label != "Renamed" {
		t.Errorf("label = %q", edited.Label)
	}

	stored, err := h.store.Proxies(t.Context())
	if err != nil || len(stored) != 1 {
		t.Fatalf("Proxies() = %v, %v", stored, err)
	}
	if stored[0].Token != "keep-me" {
		t.Errorf("an empty token wiped the stored one: %q", stored[0].Token)
	}
}

// Switched off means not used, and the fetcher is handed a list that already reflects that.
func TestASwitchedOffRelayIsNotHandedToTheFetcher(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "url": "https://proxio.example.com", "token": "t",
	}), http.StatusCreated, &added)

	off := false
	h.expect(h.do(http.MethodPut, "/api/admin/proxies/"+added.ID, map[string]any{
		"kind": "proxio", "url": "https://proxio.example.com", "enabled": off,
	}), http.StatusOK, &proxyBody{})

	// Still listed for the administrator, so it can be switched back on without the token
	// having to be found again.
	var list proxyList
	h.expect(h.do(http.MethodGet, "/api/admin/proxies", nil), http.StatusOK, &list)
	if len(list.Proxies) != 1 || list.Proxies[0].Enabled {
		t.Fatalf("list = %+v", list.Proxies)
	}

	// And not handed to anything that would use it.
	stored, err := h.store.Proxies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Errorf("a switched-off relay was still offered to the fetcher: %+v", stored)
	}
}

// Relays are tried highest priority first, so that order has to survive being stored.
func TestRelaysComeBackInTheOrderTheyAreTried(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	for _, p := range []struct {
		label    string
		priority int
	}{{"third", 10}, {"first", 100}, {"second", 50}} {
		h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
			"kind": "proxio", "label": p.label, "url": "https://" + p.label + ".example.com",
			"token": "t", "priority": p.priority,
		}), http.StatusCreated, &proxyBody{})
	}

	var list proxyList
	h.expect(h.do(http.MethodGet, "/api/admin/proxies", nil), http.StatusOK, &list)
	want := []string{"first", "second", "third"}
	for i, p := range list.Proxies {
		if p.Label != want[i] {
			t.Errorf("place %d is %q, want %q", i, p.Label, want[i])
		}
	}

	// And the fetcher is handed them the same way round.
	stored, err := h.store.Proxies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range stored {
		if p.Label != want[i] {
			t.Errorf("the fetcher gets %q at place %d, want %q", p.Label, i, want[i])
		}
	}
}

// A relay nobody gave a priority is one somebody wants used, so it goes to the top.
func TestARelayWithNoPriorityIsWorthTheMost(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "url": "https://proxio.example.com", "token": "t",
	}), http.StatusCreated, &added)
	if added.Priority != 100 {
		t.Errorf("priority = %d, want 100", added.Priority)
	}

	// Zero is a real setting — tried last — and is not mistaken for "nobody said".
	var floor proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "url": "https://last.example.com", "token": "t", "priority": 0,
	}), http.StatusCreated, &floor)
	if floor.Priority != 0 {
		t.Errorf("priority = %d, want the 0 that was asked for", floor.Priority)
	}
	// And it is still used, because never is the off switch rather than the bottom of a range.
	stored, err := h.store.Proxies(t.Context())
	if err != nil || len(stored) != 2 {
		t.Fatalf("Proxies() = %v, %v", stored, err)
	}

	// An edit that says nothing about priority keeps the one it had.
	var edited proxyBody
	h.expect(h.do(http.MethodPut, "/api/admin/proxies/"+floor.ID, map[string]any{
		"kind": "proxio", "label": "renamed", "url": "https://last.example.com",
	}), http.StatusOK, &edited)
	if edited.Priority != 0 {
		t.Errorf("an edit that said nothing changed the priority to %d", edited.Priority)
	}
}

func TestARelayIsCheckedBeforeItIsStored(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	for _, tc := range []struct {
		what string
		body map[string]any
	}{
		{"no address", map[string]any{"kind": "proxio", "token": "t"}},
		{"no scheme", map[string]any{"kind": "proxio", "url": "proxio:80", "token": "t"}},
		{"not http", map[string]any{"kind": "proxio", "url": "ftp://proxio", "token": "t"}},
		{"no token", map[string]any{"kind": "proxio", "url": "https://proxio.example.com"}},
		{"a kind nothing can dial", map[string]any{"kind": "socks5", "url": "http://p:1080", "token": "t"}},
	} {
		res := h.do(http.MethodPost, "/api/admin/proxies", tc.body)
		if res.StatusCode == http.StatusCreated {
			t.Errorf("%s was accepted", tc.what)
		}
		res.Body.Close()
	}
}

// The path a relay serves on is this program's business, not something to be typed twice.
func TestPastingTheWholeExampleURLStillWorks(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "token": "t",
		"url": "https://proxio.example.com/proxy?token=OLD&url=https%3A%2F%2Fexample.com",
	}), http.StatusCreated, &added)

	if added.URL != "https://proxio.example.com" {
		t.Errorf("url = %q, want the address with the relay's own path dropped", added.URL)
	}
}

// Test says whether the relay works, and tells its own failures from the target's.
func TestTestingARelaySaysWhoFailed(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "good" {
			w.Header().Set("X-Proxio-Error", `{"error":"bad token"}`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer relay.Close()

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "url": relay.URL, "token": "good",
	}), http.StatusCreated, &added)

	var ok struct {
		OK     bool `json:"ok"`
		Status int  `json:"status"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/proxies/test", map[string]any{"id": added.ID}),
		http.StatusOK, &ok)
	if !ok.OK || ok.Status != http.StatusOK {
		t.Fatalf("a working relay tested as %+v", ok)
	}

	// A token that is wrong is the relay's fault, and has to be reported as the relay's
	// rather than as the target refusing.
	var bad struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/proxies/test", map[string]any{
		"id":    added.ID,
		"proxy": map[string]any{"kind": "proxio", "url": relay.URL, "token": "wrong"},
	}), http.StatusOK, &bad)
	if bad.OK {
		t.Fatal("a relay with the wrong token tested as working")
	}
	if !contains(bad.Error, "fault was its own") {
		t.Errorf("the failure does not say it was the relay's: %q", bad.Error)
	}
}

func TestOnlyAnAdminTouchesTheRelays(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleUser, "reader")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/proxies"},
		{http.MethodPost, "/api/admin/proxies"},
		{http.MethodPut, "/api/admin/proxies/px_1"},
		{http.MethodDelete, "/api/admin/proxies/px_1"},
		{http.MethodPost, "/api/admin/proxies/test"},
	} {
		res := h.do(tc.method, tc.path, map[string]any{})
		if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s answered %d for an ordinary reader", tc.method, tc.path, res.StatusCode)
		}
		res.Body.Close()
	}
}

// A SOCKS5 relay is stored with its username, and its password is as write-only as a token.
func TestASocksRelayKeepsItsPasswordToItself(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "socks5", "label": "Tunnel", "url": "socks5://socks.internal:1080",
		"username": "operator", "token": "hunter2",
	}), http.StatusCreated, &added)
	if added.Kind != "socks5" || added.Username != "operator" {
		t.Fatalf("added = %+v", added)
	}

	res := h.do(http.MethodGet, "/api/admin/proxies", nil)
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(body), "hunter2") {
		t.Errorf("the list handed the password back:\n%s", body)
	}
}

// Credentials pasted into the address are moved out of it rather than left on screen.
func TestCredentialsInASocksAddressAreMovedOutOfIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "socks5", "url": "socks5://operator:hunter2@socks.internal:1080",
	}), http.StatusCreated, &added)

	if added.URL != "socks5://socks.internal:1080" {
		t.Errorf("url = %q; the credential is still in the address", added.URL)
	}
	if added.Username != "operator" {
		t.Errorf("username = %q, want it lifted out of the address", added.Username)
	}
	stored, err := h.store.Proxies(t.Context())
	if err != nil || len(stored) != 1 {
		t.Fatalf("Proxies() = %v, %v", stored, err)
	}
	if stored[0].Token != "hunter2" {
		t.Errorf("the password was not lifted out of the address: %q", stored[0].Token)
	}
}

// Each kind's address has to be written the way that kind is dialled.
func TestEachKindWantsItsOwnAddress(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	for _, tc := range []struct {
		what string
		body map[string]any
		ok   bool
	}{
		{"proxio over http", map[string]any{"kind": "proxio", "url": "https://p.example.com", "token": "t"}, true},
		{"proxio written as socks", map[string]any{"kind": "proxio", "url": "socks5://p:1080", "token": "t"}, false},
		{"socks5 over socks5", map[string]any{"kind": "socks5", "url": "socks5://p:1080", "username": "u", "token": "t"}, true},
		{"socks5h too", map[string]any{"kind": "socks5", "url": "socks5h://p:1080", "username": "u", "token": "t"}, true},
		{"socks5 written as http", map[string]any{"kind": "socks5", "url": "http://p:1080", "username": "u", "token": "t"}, false},
		{"socks5 with no username", map[string]any{"kind": "socks5", "url": "socks5://p:1080", "token": "t"}, false},
	} {
		res := h.do(http.MethodPost, "/api/admin/proxies", tc.body)
		got := res.StatusCode == http.StatusCreated
		res.Body.Close()
		if got != tc.ok {
			t.Errorf("%s: accepted = %v, want %v", tc.what, got, tc.ok)
		}
	}
}

// Reset forgets what was learned through one relay, and nothing else.
func TestResettingARelayForgetsOnlyItsOwnRoutes(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var first, second proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "label": "one", "url": "https://one.example.com", "token": "t",
	}), http.StatusCreated, &first)
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "label": "two", "url": "https://two.example.com", "token": "t",
	}), http.StatusCreated, &second)

	for _, domain := range []string{"theguardian.com", "nbcnews.com"} {
		if err := h.store.RememberProxyRoute(t.Context(), domain, first.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.RememberProxyRoute(t.Context(), "lobste.rs", second.ID); err != nil {
		t.Fatal(err)
	}

	// The list says what each relay is carrying, which is what makes resetting one a
	// decision rather than a guess.
	var list proxyList
	h.expect(h.do(http.MethodGet, "/api/admin/proxies", nil), http.StatusOK, &list)
	byID := map[string]proxyBody{}
	for _, p := range list.Proxies {
		byID[p.ID] = p
	}
	if byID[first.ID].Routes != 2 || byID[second.ID].Routes != 1 {
		t.Fatalf("routes = %d and %d, want 2 and 1",
			byID[first.ID].Routes, byID[second.ID].Routes)
	}

	var reset struct {
		Forgotten int `json:"forgotten"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/proxies/"+first.ID+"/reset", map[string]any{}),
		http.StatusOK, &reset)
	if reset.Forgotten != 2 {
		t.Errorf("forgot %d routes, want 2", reset.Forgotten)
	}

	// The other relay's route is untouched, and so is the relay itself.
	h.expect(h.do(http.MethodGet, "/api/admin/proxies", nil), http.StatusOK, &list)
	if len(list.Proxies) != 2 {
		t.Fatalf("resetting removed a relay: %+v", list.Proxies)
	}
	for _, p := range list.Proxies {
		want := 0
		if p.ID == second.ID {
			want = 1
		}
		if p.Routes != want {
			t.Errorf("%s carries %d routes, want %d", p.Label, p.Routes, want)
		}
	}
}

// Deleting a relay takes what was learned through it, so nothing is left naming an id that
// resolves to nothing.
func TestDeletingARelayTakesItsRoutesWithIt(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var added proxyBody
	h.expect(h.do(http.MethodPost, "/api/admin/proxies", map[string]any{
		"kind": "proxio", "url": "https://one.example.com", "token": "t",
	}), http.StatusCreated, &added)
	if err := h.store.RememberProxyRoute(t.Context(), "theguardian.com", added.ID); err != nil {
		t.Fatal(err)
	}

	res := h.do(http.MethodDelete, "/api/admin/proxies/"+added.ID, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete answered %d", res.StatusCode)
	}

	counts, err := h.store.ProxyRouteCounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Errorf("routes outlived the relay they name: %v", counts)
	}
}

func TestResettingARelayThatIsNotThere(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	res := h.do(http.MethodPost, "/api/admin/proxies/px_nothing/reset", map[string]any{})
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("answered %d, want 404 rather than a cheerful nothing-forgotten", res.StatusCode)
	}
}

// A relay can be tried before it exists, which is the whole reason the id is in the body.
//
// The only way to find out whether a token works should not be to save it over one that
// already did.
func TestARelayCanBeTriedBeforeItIsSaved(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var asked string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "good" {
			w.Header().Set("X-Proxio-Error", `{"error":"bad token"}`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		asked = r.URL.Query().Get("url")
		w.WriteHeader(http.StatusOK)
	}))
	defer relay.Close()

	var out struct {
		OK     bool   `json:"ok"`
		Status int    `json:"status"`
		URL    string `json:"url"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/proxies/test", map[string]any{
		"proxy": map[string]any{"kind": "proxio", "url": relay.URL, "token": "good"},
		"url":   "https://blocked.example/feed",
	}), http.StatusOK, &out)

	if !out.OK || out.Status != http.StatusOK {
		t.Fatalf("an unsaved relay tested as %+v", out)
	}
	if asked != "https://blocked.example/feed" {
		t.Errorf("the relay was asked for %q, want the address that was typed", asked)
	}
	if out.URL != "https://blocked.example/feed" {
		t.Errorf("the answer names %q, want what was asked for", out.URL)
	}

	// And nothing was written: trying is not saving.
	var list proxyList
	h.expect(h.do(http.MethodGet, "/api/admin/proxies", nil), http.StatusOK, &list)
	if len(list.Proxies) != 0 {
		t.Errorf("trying a relay stored it: %+v", list.Proxies)
	}
}

// Left empty, the target is this instance's own address rather than somebody else's server.
func TestATestWithNoAddressFetchesThisInstance(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	var asked string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("url")
		w.WriteHeader(http.StatusOK)
	}))
	defer relay.Close()

	var out struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	h.expect(h.do(http.MethodPost, "/api/admin/proxies/test", map[string]any{
		"proxy": map[string]any{"kind": "proxio", "url": relay.URL, "token": "t"},
	}), http.StatusOK, &out)

	if !out.OK {
		t.Fatalf("out = %+v", out)
	}
	if asked == "" || asked != out.URL {
		t.Errorf("asked for %q and reported %q", asked, out.URL)
	}
	if !contains(asked, "://") {
		t.Errorf("the default target %q is not an address", asked)
	}
}

// A test with nothing to test is refused rather than guessed at.
func TestATestNeedsARelayToTry(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "root")

	res := h.do(http.MethodPost, "/api/admin/proxies/test", map[string]any{
		"url": "https://example.com",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("answered %d, want 400", res.StatusCode)
	}
}
