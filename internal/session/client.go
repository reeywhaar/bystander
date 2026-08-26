package session

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"bystander/internal/store"
)

// maxUserAgent is where a user agent is cut. Real ones run to about 150 characters; this
// leaves room for the baroque ones and refuses to store a kilobyte a stranger chose.
const maxUserAgent = 512

// device reads what a request says about where it came from.
//
// Both values are for a person to look at in a list of their own sessions. Neither is used
// to decide anything, which is the only reason it is acceptable that neither can be
// trusted: an address is what the last hop claims, and a user agent is a sentence the
// browser wrote about itself.
func device(r *http.Request) store.Device {
	return store.Device{IP: clientIP(r), UserAgent: userAgent(r)}
}

// clientIP resolves the address a request came from, through whatever proxies are in front.
//
// X-Forwarded-For is written by whoever sends it, so the question is never "what does the
// header say" but "who said it". The answer here needs no configuration: a header is only
// believed when the machine that handed us the request is on a private network or the
// loopback — which is where a reverse proxy in a compose file sits, and is not where the
// internet is. Exposed directly, the peer is public, the header is ignored, and a stranger
// cannot write their own address into somebody's session list.
//
// The chain is then walked from the right, discarding hops that are themselves private,
// because a client can prepend anything it likes to X-Forwarded-For and only the entries
// our own proxies appended are worth anything. The first public address from that end is
// the caller. If every hop is private the deployment genuinely is private, and the
// left-most entry is the closest thing to an origin there is.
//
// Returns an empty string rather than a guess when there is nothing to report.
func clientIP(r *http.Request) string {
	peer := hostOf(r.RemoteAddr)
	if peer == "" || !isInternal(peer) {
		return peer
	}

	if chain := r.Header.Get("X-Forwarded-For"); chain != "" {
		hops := strings.Split(chain, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			hop := normalise(strings.TrimSpace(hops[i]))
			if hop == "" {
				continue
			}
			if !isInternal(hop) {
				return hop
			}
		}
		// Every hop was private, including the client. Say so with the far end rather
		// than with the proxy standing next to us.
		for _, hop := range hops {
			if hop := normalise(strings.TrimSpace(hop)); hop != "" {
				return hop
			}
		}
	}

	// Not everything sends a chain. X-Real-IP holds one address and comes from the same
	// trusted peer, so it is believed on the same terms.
	if real := normalise(strings.TrimSpace(r.Header.Get("X-Real-IP"))); real != "" {
		return real
	}
	return peer
}

// userAgent returns the browser's self-description, trimmed and bounded.
func userAgent(r *http.Request) string {
	ua := strings.TrimSpace(r.Header.Get("User-Agent"))
	// Control characters would travel into a JSON document and out into a page. Nothing
	// legitimate contains them.
	ua = strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, ua)
	if len(ua) > maxUserAgent {
		ua = strings.TrimSpace(ua[:maxUserAgent])
	}
	return ua
}

// hostOf strips the port RemoteAddr always carries, and the brackets IPv6 arrives in.
func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return normalise(host)
	}
	return normalise(addr)
}

// normalise parses and reprints an address, so that a header saying "::ffff:203.0.113.7" or
// "[2001:db8::1]:443" and a peer saying the same thing are one string rather than two.
func normalise(s string) string {
	s = strings.Trim(s, "[]")
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = strings.Trim(host, "[]")
	}
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return ""
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	return ip.WithZone("").String()
}

// isInternal reports whether an address belongs to the machine or to a private network —
// which is to say, whether it is somewhere a reverse proxy of ours might be standing.
func isInternal(s string) bool {
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}
