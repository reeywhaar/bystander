package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// unredirected asks for one path without following where it points.
//
// The harness client follows redirects, which is what every other test wants; here the
// redirect *is* the answer, and following it would only tell us the feeds screen renders.
func (h *harness) unredirected(t *testing.T, path string) *http.Response {
	t.Helper()
	client := &http.Client{
		Jar: h.client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return h.doAs(client, http.MethodGet, path, nil)
}

// A subscription link lands on the screen that can do something with the address.
func TestASubscribeLinkCarriesTheAddressToTheFeedsScreen(t *testing.T) {
	h := newHarness(t)

	res := h.unredirected(t, "/subscribe?url="+url.QueryEscape("https://example.com/rss"))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("answered %d, want 303", res.StatusCode)
	}
	where, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if where.Path != ManagePath {
		t.Errorf("sent to %q, want the feeds screen", where.Path)
	}
	if got := where.Query().Get(addParam); got != "https://example.com/rss" {
		t.Errorf("carried %q, want the address that was asked for", got)
	}
	// Relative, so nothing here has to decide which host to trust.
	if where.Host != "" {
		t.Errorf("Location names a host: %q", res.Header.Get("Location"))
	}
}

// It works for somebody with no session, because the screen it lands on handles that.
//
// The alternative — refusing here — would lose the address at exactly the moment it matters:
// somebody who does not have this open is the person a subscription link is for.
func TestASubscribeLinkWorksWithoutASession(t *testing.T) {
	h := newHarness(t)

	res := h.unredirected(t, "/subscribe?url="+url.QueryEscape("https://example.com/rss"))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("a signed-out visitor got %d, want 303", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Location"), "example.com") {
		t.Errorf("the address was dropped: %q", res.Header.Get("Location"))
	}
}

// Whatever is in the address survives being put in a query string.
//
// A feed URL routinely has its own query — ?format=rss, ?tag=go — and nesting one query inside
// another is where a link quietly loses half of itself.
func TestAnAddressWithItsOwnQuerySurvives(t *testing.T) {
	h := newHarness(t)

	for _, raw := range []string{
		"https://example.com/feed?format=rss&tag=go",
		"https://example.com/feed#fragment",
		"feed:https://example.com/rss",
		"feed://example.com/rss",
		"https://example.com/rss?a=1&b=2#c",
		"https://user:pass@example.com/rss",
		"https://example.com/путь/rss",
	} {
		res := h.unredirected(t, "/subscribe?url="+url.QueryEscape(raw))
		where, err := url.Parse(res.Header.Get("Location"))
		res.Body.Close()
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got := where.Query().Get(addParam); got != raw {
			t.Errorf("%q came through as %q", raw, got)
		}
	}
}

// A link with nothing in it is still a link to the feeds screen.
func TestASubscribeLinkWithNoAddress(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/subscribe", "/subscribe?url=", "/subscribe?url=%20%20"} {
		res := h.unredirected(t, path)
		location := res.Header.Get("Location")
		res.Body.Close()

		if res.StatusCode != http.StatusSeeOther {
			t.Errorf("%s answered %d", path, res.StatusCode)
		}
		if location != ManagePath {
			t.Errorf("%s sent to %q, want the plain feeds screen", path, location)
		}
	}
}

// The address is not a way to send somebody somewhere else.
//
// It is put in a query string on a relative path, so there is nothing for it to take over —
// but this is the shape of link that gets pasted into a chat, and an open redirect wearing our
// domain is a phishing primitive.
func TestASubscribeLinkCannotSendYouOffSite(t *testing.T) {
	h := newHarness(t)

	for _, raw := range []string{
		"https://evil.example/",
		"//evil.example/",
		"/\\evil.example",
		"javascript:alert(1)",
		"https://example.com/\r\nSet-Cookie: a=b",
	} {
		res := h.unredirected(t, "/subscribe?url="+url.QueryEscape(raw))
		location := res.Header.Get("Location")
		cookie := res.Header.Get("Set-Cookie")
		res.Body.Close()

		where, err := url.Parse(location)
		if err != nil {
			t.Fatalf("%q produced an unparseable Location %q", raw, location)
		}
		if where.Scheme != "" || where.Host != "" {
			t.Errorf("%q sent somebody to %q", raw, location)
		}
		if !strings.HasPrefix(where.Path, ManagePath) {
			t.Errorf("%q sent somebody to %q", raw, location)
		}
		if cookie != "" {
			t.Errorf("%q got a header past the redirect: %q", raw, cookie)
		}
	}
}
