package opml

import (
	"regexp"
	"strings"
)

// The plain list is the other half of sharing.
//
// Somebody pasting a subscription list into a message does not paste XML at them; they
// paste something a person can read. This reads that back — loosely, on purpose. It is not
// a format with a grammar, it is whatever survived being copied out of a chat window, so
// the rule is to take what can be understood and ignore the rest.
//
// The shape it is written in, and the one it reads best:
//
//	The Go Blog
//	https://go.dev/blog/feed.atom
//	Engineering
//
//	Hi-Fructose Magazine
//	https://hifructose.com/feed/
//	Art, News / World
//
// But a bare column of addresses works, and so does "Title — https://…" on one line,
// because those are the other two things people actually paste.

// address finds a link anywhere in a line. Trailing brackets and quotes are excluded so a
// URL wrapped in punctuation comes back without it.
var address = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `\)\]]+`)

// DecodeList reads the plain form.
//
// Never fails: a line it cannot make sense of is a line it ignores. Whether anything was
// found is the caller's question to ask of the result.
func DecodeList(text string) *Document {
	doc := &Document{}

	var (
		current *Feed
		pending string // a title seen before its address
	)
	flush := func() {
		if current != nil {
			doc.Feeds = append(doc.Feeds, *current)
			current = nil
		}
	}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			pending = ""
			continue
		}

		if at := address.FindStringIndex(line); at != nil {
			flush()

			// Anything before the address on the same line is a title — after the dashes
			// and bullets people put between the two.
			title := strings.Trim(strings.TrimSpace(line[:at[0]]), " \t-—–:·|•*>")
			if title == "" {
				title = pending
			}
			current = &Feed{
				Title:    title,
				FeedURL:  line[at[0]:at[1]],
				Priority: -1,
				Reach:    -1,
			}
			pending = ""
			continue
		}

		// The line after an address is its tags. Anything further is a title waiting for
		// the next address — which is what the blank-line-separated form looks like.
		if current != nil && len(current.Categories) == 0 {
			if categories := parseListCategories(line); len(categories) > 0 {
				current.Categories = categories
				continue
			}
		}
		pending = line
	}
	flush()

	return doc
}

// parseListCategories reads "Art, News / World" as two tags, one of them nested.
//
// No escaping, unlike the category attribute: this is the readable form, and a tag with a
// comma in it is worth less than a line somebody can read. OPML is the lossless one.
func parseListCategories(line string) [][]string {
	var out [][]string
	for _, tag := range strings.Split(line, ",") {
		var path []string
		for _, segment := range strings.Split(tag, "/") {
			if segment = strings.TrimSpace(segment); segment != "" {
				path = append(path, segment)
			}
		}
		if len(path) > 0 {
			out = append(out, path)
		}
	}
	return out
}

// DecodeAny reads whichever form it was given.
//
// The two are told apart by the only thing that reliably separates them: OPML is XML and
// starts with a tag. Everything else is treated as the plain list, which cannot fail —
// so the error here always means "that was meant to be XML and is not", which is the more
// useful thing to say.
func DecodeAny(text string) (*Document, error) {
	if strings.HasPrefix(strings.TrimSpace(text), "<") {
		return Decode(strings.NewReader(text))
	}
	return DecodeList(text), nil
}
