package api

import (
	"net/http"
	"net/url"
	"strings"
)

// SubscribePath is the address other sites link to, and the one a browser hands a `feed:`
// link to when this instance is registered as a reader for it.
//
// `/subscribe?url=…` is the shape every other reader uses, which is the whole reason for
// picking it: a publisher's "subscribe in your reader" menu is a list of these, and a
// bookmarklet or a browser extension that knows the shape works here without knowing anything
// about this program.
const SubscribePath = "/subscribe"

// addParam is what the feeds screen reads the address out of once it gets there.
const addParam = "add"

// subscribe hands a feed address to the screen that can do something with it.
//
// A redirect rather than a page, because there is already a screen for this and it is the one
// somebody wants to end up on: the feed list, with the address in the box and the same
// question asked of it that pasting it there would ask. Sending them somewhere else would mean
// a second interface for adding a feed, which is a second place for the two to disagree.
//
// # Why nothing is checked here
//
// A bad address is not an error page, it is a wrong address in a box that can be corrected.
// The feeds screen already says what is wrong with one and lets somebody fix it in place;
// refusing here would replace that with a dead end and lose what they clicked.
//
// # Why there is no session check
//
// The screen this lands on has one, and it carries the whole address into `?next=` on its way
// to the login form — so a stranger who clicks a subscribe link signs in and arrives at the
// add-a-feed flow with the address still in hand. Checking here would do the same thing worse,
// by having to reimplement that.
func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	target := ManagePath

	// The address is whatever was passed, tidied of nothing but the spaces a copy-paste
	// leaves. `feed:` and its relatives are unwrapped further in, where the decision about
	// what an address is already lives — see store.CanonicalURL.
	if raw := strings.TrimSpace(r.URL.Query().Get("url")); raw != "" {
		target += "?" + url.Values{addParam: {raw}}.Encode()
	}

	// A relative Location, so nothing here has to decide which host to trust. And See Other
	// rather than Found, because the answer to "subscribe to this" is "go and look at this
	// screen" — a different resource, not a moved one.
	http.Redirect(w, r, target, http.StatusSeeOther)
}
