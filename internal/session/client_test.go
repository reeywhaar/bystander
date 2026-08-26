package session

import (
	"net/http"
	"strings"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		peer   string
		xff    string
		real   string
		expect string
	}{{
		name:   "direct from the internet",
		peer:   "203.0.113.7:51234",
		expect: "203.0.113.7",
	}, {
		name: "a public peer's headers are not believed",
		peer: "203.0.113.7:51234",
		xff:  "10.0.0.1, 198.51.100.9",
		real: "198.51.100.9",
		// The machine talking to us is on the internet, so it is not a proxy of ours and
		// nothing it says about earlier hops means anything.
		expect: "203.0.113.7",
	}, {
		name:   "behind a proxy on the loopback",
		peer:   "127.0.0.1:39000",
		xff:    "198.51.100.9",
		expect: "198.51.100.9",
	}, {
		name:   "behind a proxy on a docker network",
		peer:   "172.18.0.4:39000",
		xff:    "198.51.100.9, 172.18.0.3",
		expect: "198.51.100.9",
	}, {
		name: "a client's forged prefix is discarded",
		peer: "172.18.0.4:39000",
		// The client sent "1.2.3.4" itself; our proxies appended the rest. Walking from
		// the right and stopping at the first public hop finds the real caller.
		xff:    "1.2.3.4, 198.51.100.9, 172.18.0.3",
		expect: "198.51.100.9",
	}, {
		name:   "x-real-ip when there is no chain",
		peer:   "127.0.0.1:39000",
		real:   "198.51.100.9",
		expect: "198.51.100.9",
	}, {
		name: "a wholly private deployment",
		peer: "192.168.1.10:39000",
		xff:  "192.168.1.55",
		// Nothing in the chain is public, so the far end is the closest thing to an
		// origin there is.
		expect: "192.168.1.55",
	}, {
		name:   "a proxy with nothing to add",
		peer:   "127.0.0.1:39000",
		expect: "127.0.0.1",
	}, {
		name:   "ipv6 is unbracketed",
		peer:   "[2001:db8::1]:443",
		expect: "2001:db8::1",
	}, {
		name:   "an ipv4-mapped hop is reported as ipv4",
		peer:   "127.0.0.1:39000",
		xff:    "::ffff:198.51.100.9",
		expect: "198.51.100.9",
	}, {
		name:   "nonsense in the chain is skipped",
		peer:   "127.0.0.1:39000",
		xff:    "unknown, 198.51.100.9, not-an-address",
		expect: "198.51.100.9",
	}, {
		name:   "nothing believable at all",
		peer:   "127.0.0.1:39000",
		xff:    "unknown",
		expect: "127.0.0.1",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.RemoteAddr = c.peer
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if c.real != "" {
				r.Header.Set("X-Real-IP", c.real)
			}
			if got := clientIP(r); got != c.expect {
				t.Errorf("clientIP() = %q, want %q", got, c.expect)
			}
		})
	}
}

func TestUserAgentIsBounded(t *testing.T) {
	r, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	r.Header.Set("User-Agent", "  Mozilla/5.0  ")
	if got := userAgent(r); got != "Mozilla/5.0" {
		t.Errorf("userAgent() = %q, want it trimmed", got)
	}

	// A kilobyte a stranger chose is not a thing to keep a row of.
	r.Header.Set("User-Agent", strings.Repeat("x", 4000))
	if got := userAgent(r); len(got) != maxUserAgent {
		t.Errorf("userAgent() kept %d bytes, want %d", len(got), maxUserAgent)
	}

	// Control characters would travel into a JSON document and out into a page.
	r.Header.Set("User-Agent", "Mozilla\x00/5.0\x1b[31m")
	if got := userAgent(r); got != "Mozilla/5.0[31m" {
		t.Errorf("userAgent() = %q, want the control characters gone", got)
	}
}
