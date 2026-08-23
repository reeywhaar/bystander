package feeds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"bystander/internal/store"
)

const (
	// requestTimeout bounds one fetch, redirects included — including reading the body,
	// which is where slow publishers actually spend the time. Thirty seconds because a
	// tired shared host answering a hundred-kilobyte feed genuinely takes tens of seconds
	// on a cold cache and under a second once warm, and failing the first fetch of a feed
	// somebody has just chosen is the worst possible moment to be strict.
	requestTimeout = 30 * time.Second

	// maxBody is what will be read from a feed. Large enough for the biggest real feeds;
	// small enough that a misconfigured server streaming a gigabyte is a failed fetch
	// rather than an outage.
	maxBody = 8 << 20

	// maxRedirects is how far a feed may move. Enough for http→https plus a hostname
	// change; not enough to be walked around a redirect loop.
	maxRedirects = 5
)

// ErrNotAFeed is returned when a URL answers with something that is not a feed and names
// no feed of its own.
var ErrNotAFeed = errors.New("that URL is not a feed")

// Fetcher retrieves and parses feeds.
type Fetcher struct {
	client    *http.Client
	userAgent string
}

// NewFetcher builds a fetcher. publicURL goes in the User-Agent, so a publisher wondering
// what is hitting them can find out — which is the difference between being rate-limited
// and being blocked.
func NewFetcher(publicURL string) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("stopped after %d redirects", maxRedirects)
				}
				return nil
			},
		},
		userAgent: "bystander/1.0 (+" + publicURL + ")",
	}
}

// Result is one fetch.
type Result struct {
	Status       int
	NotModified  bool
	ETag         string
	LastModified string
	FinalURL     string
	Parsed       *Parsed
}

// Fetch retrieves a feed, sending back whatever validators it gave us last time.
//
// A 304 costs one round trip and no parsing, which is most fetches once a feed has been
// followed for a day. Publishers notice when it is skipped.
func (f *Fetcher) Fetch(ctx context.Context, feed *store.Feed, now time.Time) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.CanonicalURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/feed+json, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.8")
	if feed.ETag != "" {
		req.Header.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		req.Header.Set("If-Modified-Since", feed.LastModified)
	}

	res, err := f.client.Do(req)
	if err != nil {
		return nil, unreachable(feed.CanonicalURL, err)
	}
	defer res.Body.Close()

	result := &Result{
		Status:       res.StatusCode,
		ETag:         res.Header.Get("ETag"),
		LastModified: res.Header.Get("Last-Modified"),
		FinalURL:     res.Request.URL.String(),
	}

	if res.StatusCode == http.StatusNotModified {
		result.NotModified = true
		// Carry the previous validators forward: a 304 need not repeat them, and storing
		// the empty strings would turn every subsequent fetch into a full one.
		if result.ETag == "" {
			result.ETag = feed.ETag
		}
		if result.LastModified == "" {
			result.LastModified = feed.LastModified
		}
		return result, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return result, fmt.Errorf("the server answered %s", res.Status)
	}

	parsed, err := Parse(io.LimitReader(res.Body, maxBody), result.FinalURL, feed.ID, now)
	if err != nil {
		return result, err
	}
	result.Parsed = parsed
	return result, nil
}

// Candidate is a feed that a URL offers.
type Candidate struct {
	// URL is absolute and ready to subscribe to.
	URL string
	// Title is the publisher's own label. From the feed itself when the URL was a feed,
	// and from the <link title="…"> attribute when a page named it — which is how a site
	// distinguishes "Posts" from "Comments" from "Podcast".
	Title string
	// Type is the content type the page declared, empty when the URL was itself a feed.
	Type string
}

// Discovery is what a URL turned out to be.
type Discovery struct {
	// Feed is the parsed feed when the URL was one itself, so nothing has to fetch it a
	// second time to find that out.
	Feed    *Parsed
	FeedURL string

	// Candidates is every feed on offer, in the order the page named them. Exactly one
	// entry when the URL was itself a feed.
	Candidates []Candidate
}

// Discover works out what somebody typed.
//
// A feed URL is a feed. A web page is a page that may name feeds, and it usually names more
// than one — posts, comments, a podcast, a per-category feed. Returning all of them lets
// somebody choose rather than being handed whichever came first in the markup, which is
// how you end up subscribed to a comments feed you did not want.
func (f *Fetcher) Discover(ctx context.Context, rawURL string, now time.Time) (*Discovery, error) {
	canonical, err := store.CanonicalURL(rawURL)
	if err != nil {
		return nil, err
	}

	body, finalURL, err := f.get(ctx, canonical)
	if err != nil {
		return nil, err
	}

	// gofeed refuses an HTML page outright, but it is lenient enough that a page with one
	// stray XML-ish tag can parse into an empty shell. Requiring a title or an article is
	// what tells "a feed with nothing in it yet" from "not a feed".
	if parsed, err := Parse(strings.NewReader(body), finalURL, "", now); err == nil &&
		(len(parsed.Items) > 0 || parsed.Title != "") {
		return &Discovery{
			Feed:       parsed,
			FeedURL:    finalURL,
			Candidates: []Candidate{{URL: finalURL, Title: parsed.Title}},
		}, nil
	}

	candidates := feedLinks(body, finalURL)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotAFeed, canonical)
	}
	return &Discovery{Candidates: candidates}, nil
}

// Resolve turns what somebody typed into one feed, ready to subscribe to.
//
// Takes the first candidate when a page names several. The interface asks rather than
// guessing — see Discover — but this is what a direct POST to /api/feeds does, and
// guessing beats refusing for a caller that did not ask to be consulted.
func (f *Fetcher) Resolve(ctx context.Context, rawURL string, now time.Time) (string, *Parsed, error) {
	found, err := f.Discover(ctx, rawURL, now)
	if err != nil {
		return "", nil, err
	}
	if found.Feed != nil {
		return found.FeedURL, found.Feed, nil
	}

	first := found.Candidates[0]
	body, finalURL, err := f.get(ctx, first.URL)
	if err != nil {
		return "", nil, err
	}
	parsed, err := Parse(strings.NewReader(body), finalURL, "", now)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s names a feed at %s, but it did not parse",
			ErrNotAFeed, rawURL, first.URL)
	}
	return finalURL, parsed, nil
}

func (f *Fetcher) get(ctx context.Context, target string) (body, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/feed+json, text/html;q=0.8, */*;q=0.5")

	res, err := f.client.Do(req)
	if err != nil {
		return "", "", unreachable(target, err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("%s answered %s", target, res.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return "", "", unreachable(target, err)
	}
	return string(raw), res.Request.URL.String(), nil
}

// unreachable turns a transport failure into a sentence.
//
// Go's own text for a timeout is "context deadline exceeded (Client.Timeout or context
// cancellation while reading body)", which is accurate and belongs in a log. It reaches
// somebody who has just pasted an address and pressed a button, and it has to tell them
// what to do about it — which is usually "try again", because a publisher that stalls once
// is often fine a minute later.
func unreachable(target string, err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%s did not answer within %s; it may be slow rather than broken, so it is worth trying again",
			target, requestTimeout)
	}
	return fmt.Errorf("could not reach %s: %w", target, err)
}

// feedTypes are the content types a <link rel="alternate"> may name for this to be a feed.
//
// Not "application/json". JSON Feed's own type is "application/feed+json"; plain
// application/json is what WordPress declares for its REST representation of a page, which
// means every WordPress site on the internet — a large fraction of them — would offer a
// wp-json endpoint as a feed to subscribe to. Somebody pasting a JSON feed's URL directly
// still gets it, because that path parses the body rather than trusting a declared type.
var feedTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/feed+json": true,
	"text/xml":              true,
	"application/xml":       true,
}

// feedLinks finds every feed a web page names, in the order it named them.
//
// Deduplicated by URL, because a page that declares the same feed in two places — once for
// the browser and once for a reader extension — is naming one feed, not two.
func feedLinks(document, base string) []Candidate {
	baseURL, _ := url.Parse(base)

	var out []Candidate
	seen := make(map[string]bool)

	tokenizer := html.NewTokenizer(strings.NewReader(document))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			if string(name) != "link" || !hasAttr {
				continue
			}
			var rel, ctype, href, title string
			for {
				key, value, more := tokenizer.TagAttr()
				switch string(key) {
				case "rel":
					rel = strings.ToLower(string(value))
				case "type":
					ctype = strings.ToLower(strings.TrimSpace(string(value)))
				case "href":
					href = string(value)
				case "title":
					title = strings.TrimSpace(string(value))
				}
				if !more {
					break
				}
			}
			if !strings.Contains(rel, "alternate") || !feedTypes[ctype] || href == "" {
				continue
			}
			resolved := safeURL(href, baseURL)
			if resolved == "" || seen[resolved] {
				continue
			}
			seen[resolved] = true
			out = append(out, Candidate{URL: resolved, Title: title, Type: ctype})
		}
	}
}
