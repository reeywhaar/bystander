package feeds

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"bystander/internal/store"
)

// Parsed is what one fetch produced.
type Parsed struct {
	Title   string
	SiteURL string
	Items   []*store.Item
}

// Parse reads a feed body into articles.
//
// gofeed handles RSS, Atom and JSON Feed behind one type, which is the reason to take the
// dependency at all: the alternative is three parsers and a format sniffer that has to
// agree with all of them.
func Parse(body io.Reader, feedURL, feedID string, now time.Time) (*Parsed, error) {
	parsed, err := gofeed.NewParser().Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	// Relative links in a feed resolve against the feed's own URL, not the site's: a feed
	// at /blog/feed.xml linking to "post-1" means /blog/post-1.
	base, _ := url.Parse(feedURL)

	out := &Parsed{
		Title: strings.TrimSpace(parsed.Title),
		Items: make([]*store.Item, 0, len(parsed.Items)),
	}
	// A feed's declared link is often just rel="self" — the Go Blog's is exactly that, and
	// it has no alternate at all. Treating that as the site would give every card a source
	// link that downloads an XML file. An empty SiteURL is honest: the interface names the
	// source without linking it.
	if site := safeURL(parsed.Link, base); site != feedURL {
		out.SiteURL = site
	}

	for _, entry := range parsed.Items {
		item := &store.Item{
			// No id. An article's is derived from the feed it belongs to, and this
			// function is also called during discovery — before there is a feed row, with
			// an empty feed id — so naming articles here would name two different feeds'
			// articles identically. The store assigns it, where the feed id is real.
			FeedID:    feedID,
			GUID:      identify(entry),
			Title:     strings.TrimSpace(Text(entry.Title)),
			Link:      safeURL(entry.Link, base),
			Author:    author(entry),
			Summary:   Sanitize(summaryOf(entry), feedURL),
			ImageURL:  imageOf(entry, base),
			FetchedAt: now,
		}
		// A feed that gives no date at all would otherwise need a null published_at
		// handled at every point that orders by it. Falling back to now means an article
		// with no date sorts as new, which is the least wrong thing to assume about
		// something that has only just appeared.
		item.PublishedAt = published(entry, now)

		// An article with no title and no link is not something a card can show or a
		// reader can open. Dropping it here keeps that judgement in one place.
		if item.Title == "" && item.Link == "" {
			continue
		}
		if item.Title == "" {
			item.Title = item.Link
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// identify is an article's identity within its feed.
//
// The publisher's guid when there is one, the link when there is not, and a digest of
// title and date as a last resort. What matters is that it is stable across fetches: the
// unique (feed_id, guid) index is what makes re-fetching idempotent, so a feed that
// republishes its whole window every hour produces no duplicates.
func identify(entry *gofeed.Item) string {
	if guid := strings.TrimSpace(entry.GUID); guid != "" {
		return guid
	}
	if link := strings.TrimSpace(entry.Link); link != "" {
		return link
	}
	sum := sha256.Sum256([]byte(entry.Title + "\x00" + entry.Published))
	return "sha256:" + hex.EncodeToString(sum[:16])
}

func published(entry *gofeed.Item, now time.Time) time.Time {
	if entry.PublishedParsed != nil {
		return entry.PublishedParsed.UTC()
	}
	if entry.UpdatedParsed != nil {
		return entry.UpdatedParsed.UTC()
	}
	return now
}

func author(entry *gofeed.Item) string {
	if len(entry.Authors) > 0 {
		return strings.TrimSpace(entry.Authors[0].Name)
	}
	return ""
}

// summaryOf prefers the description over the content.
//
// A description is what the publisher wrote to stand in for the article; content is the
// article itself. On a card the first is the right thing and the second is five hundred
// runes of a truncated body.
func summaryOf(entry *gofeed.Item) string {
	if strings.TrimSpace(entry.Description) != "" {
		return entry.Description
	}
	return entry.Content
}

// imageOf finds a picture for the card, which is most of what makes the page look like a
// newspaper rather than a list. Three fallbacks, in the order publishers actually use.
func imageOf(entry *gofeed.Item, base *url.URL) string {
	if entry.Image != nil {
		if u := safeURL(entry.Image.URL, base); u != "" {
			return u
		}
	}
	for _, enc := range entry.Enclosures {
		if strings.HasPrefix(enc.Type, "image/") {
			if u := safeURL(enc.URL, base); u != "" {
				return u
			}
		}
	}
	if u := firstImage(entry.Content, base); u != "" {
		return u
	}
	return firstImage(entry.Description, base)
}

// firstImage returns the src of the first <img> in a fragment.
func firstImage(fragment string, base *url.URL) string {
	if fragment == "" {
		return ""
	}
	tokenizer := html.NewTokenizer(strings.NewReader(fragment))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			if string(name) != "img" || !hasAttr {
				continue
			}
			for {
				key, value, more := tokenizer.TagAttr()
				if string(key) == "src" {
					if u := safeURL(string(value), base); u != "" {
						return u
					}
					break
				}
				if !more {
					break
				}
			}
		}
	}
}
