package api

import "strings"

// describeAgent summarises a user agent as a browser and a platform.
//
// A guess, and shown as one: the raw string travels beside it, and anything this does not
// recognise gets an empty summary rather than an invented one. The point is only to make a
// list of sessions scannable — "Safari on iPhone" is the line that tells somebody at a
// glance that the third row is their phone. Nothing decides anything on this.
//
// User agents are a museum of compatibility lies. Every mainstream browser claims to be
// Mozilla, most claim to be Safari, and several claim to be Chrome, so the checks run
// specific-to-general and stop at the first that fits — Edge before Chrome, Chrome before
// Safari — which is the only order in which any of it is true.
func describeAgent(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return ""
	}
	browser, platform := browserOf(ua), platformOf(ua)
	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case browser != "":
		return browser
	default:
		return platform
	}
}

func browserOf(ua string) string {
	for _, candidate := range []struct{ token, name string }{
		// Anything built on Chromium says "Chrome" somewhere, so its own token has to
		// be looked for first or every one of them is reported as Chrome.
		{"Edg/", "Edge"},
		{"EdgiOS/", "Edge"},
		{"OPR/", "Opera"},
		{"Vivaldi", "Vivaldi"},
		{"YaBrowser", "Yandex Browser"},
		{"SamsungBrowser", "Samsung Internet"},
		{"DuckDuckGo", "DuckDuckGo"},
		// On iOS these are Safari underneath — the token is the only thing that says
		// which app the tab is in, which is what somebody recognising a session needs.
		{"CriOS/", "Chrome"},
		{"FxiOS/", "Firefox"},
		{"Firefox/", "Firefox"},
		{"Chrome/", "Chrome"},
		{"Chromium/", "Chromium"},
		{"Safari/", "Safari"},
		// Not browsers, and worth naming when one turns up in a session list.
		{"curl/", "curl"},
		{"Wget/", "Wget"},
		{"python-requests", "python-requests"},
		{"Go-http-client", "Go"},
	} {
		if strings.Contains(ua, candidate.token) {
			return candidate.name
		}
	}
	return ""
}

func platformOf(ua string) string {
	for _, candidate := range []struct{ token, name string }{
		// Before Linux: Android and ChromeOS both say Linux too.
		{"Android", "Android"},
		{"CrOS", "ChromeOS"},
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"iPod", "iPod"},
		{"Macintosh", "Mac"},
		{"Mac OS X", "Mac"},
		{"Windows", "Windows"},
		{"FreeBSD", "FreeBSD"},
		{"OpenBSD", "OpenBSD"},
		{"Linux", "Linux"},
	} {
		if strings.Contains(ua, candidate.token) {
			return candidate.name
		}
	}
	return ""
}
