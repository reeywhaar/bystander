package feeds

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// TestARealProxioAcceptsOurNoncedToken proves the value against the thing that judges it.
//
// Skipped unless PROXIO_URL and PROXIO_TOKEN are set. The unit tests check the value against
// proxio's documented recipe, which is a second opinion but still only a reading of the docs;
// this one hands it to proxio and finds out.
//
//	docker run -d -p 18081:80 -v <data>:/data <proxio> serve
//	PROXIO_URL=http://localhost:18081 PROXIO_TOKEN=<secret> \
//	  PROXIO_HOST=$(ipconfig getifaddr en0) \
//	  go test ./internal/feeds/ -run RealNonce -v
func TestARealProxioAcceptsOurNoncedToken(t *testing.T) {
	relayURL, secret := os.Getenv("PROXIO_URL"), os.Getenv("PROXIO_TOKEN")
	host := os.Getenv("PROXIO_HOST")
	if relayURL == "" || secret == "" || host == "" {
		t.Skip("set PROXIO_URL, PROXIO_TOKEN and PROXIO_HOST to run this against a container")
	}

	var arrived string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived = r.URL.String()
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(feedXML))
	})
	listener := listenEverywhere(t)
	origin := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	origin.Start()
	defer origin.Close()

	target := "http://" + host + ":" + portOf(t, listener.Addr().String())
	proxy := &store.Proxy{ID: "px_1", Kind: store.ProxioKind, Label: "real proxio",
		URL: relayURL, Token: secret, Enabled: true}

	res, err := TestProxy(t.Context(), proxy, target)
	if err != nil {
		t.Fatalf("a real proxio refused our nonced token: %v", err)
	}
	if res != http.StatusOK {
		t.Fatalf("answered %d, want 200", res)
	}
	if arrived == "" {
		t.Fatal("the request never reached the publisher")
	}
	t.Logf("proxio accepted it and fetched %s", arrived)

	// And the secret itself is refused nowhere near the wire: build what we send and check
	// the raw value is not in it.
	sent := nonced(secret, time.Now())
	if strings.Contains(sent, secret) {
		t.Errorf("the wire value carries the secret: %q", sent)
	}
	if !strings.HasPrefix(sent, "pxc_") {
		t.Errorf("we are still sending the raw kind: %q", sent)
	}
}
