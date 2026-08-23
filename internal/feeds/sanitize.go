package feeds

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// MaxSummaryRunes is how much of a summary is kept.
//
// A card on a front page shows a standfirst, not an article. Truncating here rather than
// in the interface means the bytes are never stored, never sent, and never have to be
// re-truncated by every renderer that meets them.
const MaxSummaryRunes = 500

// allowed is the entire set of tags a summary may contain.
//
// Small on purpose. These are the tags that make a paragraph of prose readable; everything
// else a publisher puts in a description — tables, headings, figures, their own styling —
// is either noise in a card or a way to break the layout.
var allowed = map[string]bool{
	"p": true, "br": true, "em": true, "i": true, "strong": true, "b": true,
	"a": true, "ul": true, "ol": true, "li": true, "blockquote": true, "code": true,
}

// void tags have no closing tag and must not be pushed onto the stack.
var void = map[string]bool{"br": true}

// dropped tags take their contents with them. Everything else that is not allowed is
// unwrapped — the tag goes, the words inside it stay — because a publisher wrapping a
// paragraph in a div should not cost the paragraph.
var dropped = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true,
	"noscript": true, "template": true, "svg": true, "math": true, "form": true,
}

// Sanitize turns a publisher's HTML into something safe to render.
//
// This runs at ingest, once, and what it produces is what goes in the database — so every
// reader of that table gets the safe form by construction, and a bug in a renderer cannot
// become an injection. Nothing downstream sanitizes again: a second sanitizer is a second
// thing to be wrong.
//
// base is the feed's own URL, used to resolve relative links. An unresolvable or
// non-http(s) href is dropped rather than kept, which takes javascript: with it.
func Sanitize(raw, base string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	baseURL, _ := url.Parse(base)

	var (
		out   strings.Builder
		open  []string
		runes int
		skip  string // the tag whose contents are being discarded
		depth int
	)

	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return finish(&out, open)

		case html.TextToken:
			if skip != "" {
				continue
			}
			text := string(tokenizer.Text())
			remaining := MaxSummaryRunes - runes
			if remaining <= 0 {
				return finish(&out, open)
			}
			if n := len([]rune(text)); n > remaining {
				out.WriteString(html.EscapeString(string([]rune(text)[:remaining]) + "…"))
				return finish(&out, open)
			} else {
				runes += n
			}
			out.WriteString(html.EscapeString(text))

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			tag := string(name)

			if skip != "" {
				if tag == skip {
					depth++
				}
				continue
			}
			if dropped[tag] {
				skip, depth = tag, 1
				continue
			}
			if !allowed[tag] {
				continue // unwrap: drop the tag, keep what is inside it
			}

			out.WriteString("<" + tag)
			if tag == "a" && hasAttr {
				if href := resolveHref(tokenizer, baseURL); href != "" {
					// rel is not decoration: these links point at strangers' sites, and
					// noopener is what stops the page they open reaching back.
					out.WriteString(` href="` + html.EscapeString(href) + `" rel="noopener noreferrer" target="_blank"`)
				}
			}
			out.WriteString(">")

			if !void[tag] {
				open = append(open, tag)
			}

		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)

			if skip != "" {
				if tag == skip {
					depth--
					if depth == 0 {
						skip = ""
					}
				}
				continue
			}
			// Close only if it matches what is actually open. A publisher's stray </div>
			// or mismatched </b> must not close a tag this function opened.
			if n := len(open); n > 0 && open[n-1] == tag {
				out.WriteString("</" + tag + ">")
				open = open[:n-1]
			}
		}
	}
}

// finish closes whatever is still open, innermost first, so the result is balanced however
// the input ended or wherever truncation landed.
func finish(out *strings.Builder, open []string) string {
	for i := len(open) - 1; i >= 0; i-- {
		out.WriteString("</" + open[i] + ">")
	}
	return strings.TrimSpace(out.String())
}

// resolveHref returns an absolute http(s) or mailto href, or "".
func resolveHref(tokenizer *html.Tokenizer, base *url.URL) string {
	for {
		key, value, more := tokenizer.TagAttr()
		if string(key) == "href" {
			return safeURL(string(value), base)
		}
		if !more {
			return ""
		}
	}
}

// safeURL resolves a link and refuses every scheme that is not a link to a page.
//
// An allowlist rather than a javascript: blocklist: data:, vbscript: and whatever comes
// next are all refused by not being on it.
func safeURL(raw string, base *url.URL) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return u.String()
	default:
		return ""
	}
}

// Text strips a fragment of HTML to plain words, for the places a summary has to be
// compared or measured rather than rendered.
func Text(raw string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(out.String()), " ")
		case html.TextToken:
			out.Write(tokenizer.Text())
			out.WriteByte(' ')
		}
	}
}
