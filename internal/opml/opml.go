// Package opml reads and writes subscription lists.
//
// # What this emits, and why it is flat
//
// OPML has two ways to say which group a feed belongs to. Outlines can nest, which every
// reader renders as folders — but nesting is a tree, so a feed can be in exactly one
// folder, and a feed here can carry several tags. The `category` attribute has no such
// limit: it is a comma-separated list of slash-delimited paths, and the spec's own example
// is
//
//	category="/Philosophy/Baseball/Mets,/Tourism/New York"
//
// which is precisely this program's model — several tags per feed, each one possibly
// nested. So the writer emits a flat list and puts the whole truth in `category`.
//
// The reader is deliberately more forgiving than the writer. A file exported from another
// reader almost certainly uses folders, and refusing it because we would not have written
// it that way would be a poor way to meet somebody's subscription list. So it accepts
// both, and takes the ancestor folders as categories when a feed names none of its own.
package opml

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// PriorityAttr carries a feed's priority, which OPML has no field for.
//
// Deliberately not namespaced. A namespace would be more correct and would mean fighting
// encoding/xml over prefixes to gain nothing: OPML says an outline "may contain any number
// of arbitrary attributes", every other reader will ignore this one whatever it is called,
// and a name nobody else would pick is enough to make it unambiguous.
const PriorityAttr = "bystanderPriority"

// Feed is one subscription in a list.
type Feed struct {
	Title   string
	FeedURL string
	SiteURL string

	// Categories are the tags on this feed, each as a path from the root: {"News",
	// "World"} for a tag "World" nested under "News".
	Categories [][]string

	// Priority is 0..100, or -1 when the file did not say.
	Priority int
}

// Document is a whole subscription list.
type Document struct {
	Title     string
	OwnerName string
	CreatedAt time.Time
	Feeds     []Feed
}

// --- the wire shapes -------------------------------------------------------------------

type document struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    head     `xml:"head"`
	Body    body     `xml:"body"`
}

type head struct {
	Title       string `xml:"title,omitempty"`
	DateCreated string `xml:"dateCreated,omitempty"`
	OwnerName   string `xml:"ownerName,omitempty"`
}

type body struct {
	Outlines []outline `xml:"outline"`
}

type outline struct {
	Text     string `xml:"text,attr"`
	Title    string `xml:"title,attr,omitempty"`
	Type     string `xml:"type,attr,omitempty"`
	XMLURL   string `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string `xml:"htmlUrl,attr,omitempty"`
	Category string `xml:"category,attr,omitempty"`
	// Named through a struct tag rather than a Go field name, so the attribute is exactly
	// PriorityAttr and stays that way if the constant moves.
	Priority string    `xml:"bystanderPriority,attr,omitempty"`
	Children []outline `xml:"outline"`
}

// --- writing ---------------------------------------------------------------------------

// Encode writes a subscription list.
func Encode(w io.Writer, doc Document) error {
	out := document{
		Version: "2.0",
		Head: head{
			Title:     doc.Title,
			OwnerName: doc.OwnerName,
		},
	}
	if !doc.CreatedAt.IsZero() {
		// RFC 1123 in GMT, which is what the spec's own examples use.
		out.Head.DateCreated = doc.CreatedAt.UTC().Format(time.RFC1123)
	}

	for _, feed := range doc.Feeds {
		entry := outline{
			// `text` is what a reader displays and the one attribute OPML requires;
			// `title` repeats it because half the readers in the world use one and half
			// use the other.
			Text:    feed.Title,
			Title:   feed.Title,
			Type:    "rss",
			XMLURL:  feed.FeedURL,
			HTMLURL: feed.SiteURL,
		}
		if categories := EncodeCategories(feed.Categories); categories != "" {
			entry.Category = categories
		}
		if feed.Priority >= 0 {
			entry.Priority = fmt.Sprint(feed.Priority)
		}
		out.Body.Outlines = append(out.Body.Outlines, entry)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "\t")
	if err := encoder.Encode(out); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// EncodeCategories renders tag paths as OPML's category attribute.
func EncodeCategories(categories [][]string) string {
	var paths []string
	for _, path := range categories {
		if len(path) == 0 {
			continue
		}
		segments := make([]string, 0, len(path))
		for _, segment := range path {
			if segment = strings.TrimSpace(segment); segment != "" {
				segments = append(segments, escapeSegment(segment))
			}
		}
		if len(segments) > 0 {
			paths = append(paths, "/"+strings.Join(segments, "/"))
		}
	}
	return strings.Join(paths, ",")
}

// ParseCategories reads OPML's category attribute back into tag paths.
func ParseCategories(attr string) [][]string {
	var out [][]string
	for _, path := range strings.Split(attr, ",") {
		var segments []string
		for _, segment := range strings.Split(path, "/") {
			if segment = strings.TrimSpace(unescapeSegment(segment)); segment != "" {
				segments = append(segments, segment)
			}
		}
		if len(segments) > 0 {
			out = append(out, segments)
		}
	}
	return out
}

// escapeSegment protects the two characters the category attribute uses as punctuation.
//
// OPML defines no escaping — a comma separates categories and a slash separates the
// segments of one, and a tag genuinely called "Art, Design" has nowhere to go. Percent
// encoding those two characters and nothing else keeps the common case unchanged and
// readable, and makes the uncommon case survive a round trip instead of quietly splitting
// into two tags.
func escapeSegment(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, ",", "%2C")
	return strings.ReplaceAll(s, "/", "%2F")
}

func unescapeSegment(s string) string {
	s = strings.ReplaceAll(s, "%2F", "/")
	s = strings.ReplaceAll(s, "%2f", "/")
	s = strings.ReplaceAll(s, "%2C", ",")
	s = strings.ReplaceAll(s, "%2c", ",")
	return strings.ReplaceAll(s, "%25", "%")
}

// --- reading ---------------------------------------------------------------------------

// MaxDocument is the largest list this will read. A thousand feeds with long descriptions
// is well under a megabyte; anything past this is not a subscription list.
const MaxDocument = 4 << 20

// Decode reads a subscription list.
//
// Accepts nesting even though Encode does not produce it: a file from another reader will
// almost certainly use folders, and refusing it because we would have written it
// differently is a poor way to meet somebody's subscriptions. A feed inside folders and
// naming no category of its own takes the folders as its category.
func Decode(r io.Reader) (*Document, error) {
	var parsed document
	decoder := xml.NewDecoder(io.LimitReader(r, MaxDocument))
	// Feed exports are full of Latin-1 and Windows-1252. Passing the bytes through
	// unconverted keeps a mis-encoded title readable-ish instead of failing the whole
	// import over one apostrophe.
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("that does not read as OPML: %w", err)
	}

	doc := &Document{
		Title:     parsed.Head.Title,
		OwnerName: parsed.Head.OwnerName,
	}
	for _, entry := range parsed.Body.Outlines {
		collect(entry, nil, &doc.Feeds)
	}
	return doc, nil
}

// collect walks the outline tree, gathering feeds and remembering the folders above them.
func collect(entry outline, folders []string, into *[]Feed) {
	label := strings.TrimSpace(entry.Text)
	if label == "" {
		label = strings.TrimSpace(entry.Title)
	}

	if url := strings.TrimSpace(entry.XMLURL); url != "" {
		feed := Feed{
			Title:      label,
			FeedURL:    url,
			SiteURL:    strings.TrimSpace(entry.HTMLURL),
			Categories: ParseCategories(entry.Category),
			Priority:   -1,
		}
		// The folders a feed sits in are only used when it names no category itself. A
		// file that carries both is telling us the same thing twice, and the explicit
		// attribute is the one that can express more than a single path.
		if len(feed.Categories) == 0 && len(folders) > 0 {
			feed.Categories = [][]string{append([]string(nil), folders...)}
		}
		if entry.Priority != "" {
			var priority int
			if _, err := fmt.Sscanf(entry.Priority, "%d", &priority); err == nil &&
				priority >= 0 && priority <= 100 {
				feed.Priority = priority
			}
		}
		*into = append(*into, feed)
	}

	if len(entry.Children) == 0 {
		return
	}
	// An outline with children and no xmlUrl is a folder; one with both is a feed that
	// also happens to contain things, and its own label should not become a folder for
	// them.
	within := folders
	if entry.XMLURL == "" && label != "" {
		within = append(append([]string(nil), folders...), label)
	}
	for _, child := range entry.Children {
		collect(child, within, into)
	}
}
