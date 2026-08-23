package opml

import (
	"strings"
	"testing"
)

// The shape this program hands out, read back.
func TestReadsTheListItWrites(t *testing.T) {
	const shared = `The Go Blog
https://go.dev/blog/feed.atom
Engineering

Hi-Fructose Magazine
https://hifructose.com/feed/
Art, News / World`

	doc, err := DecodeAny(shared)
	if err != nil {
		t.Fatalf("DecodeAny(): %v", err)
	}
	if len(doc.Feeds) != 2 {
		t.Fatalf("%d feeds, want 2: %+v", len(doc.Feeds), doc.Feeds)
	}

	if doc.Feeds[0].Title != "The Go Blog" || doc.Feeds[0].FeedURL != "https://go.dev/blog/feed.atom" {
		t.Errorf("first = %+v", doc.Feeds[0])
	}
	if got := doc.Feeds[0].Categories; len(got) != 1 || got[0][0] != "Engineering" {
		t.Errorf("first categories = %v", got)
	}

	second := doc.Feeds[1]
	if len(second.Categories) != 2 {
		t.Fatalf("second categories = %v, want two tags", second.Categories)
	}
	if second.Categories[0][0] != "Art" {
		t.Errorf("first tag = %v", second.Categories[0])
	}
	if strings.Join(second.Categories[1], "/") != "News/World" {
		t.Errorf("second tag = %v, want the nested path", second.Categories[1])
	}
}

// The other two things people actually paste.
func TestReadsWhateverShapeItArrivesIn(t *testing.T) {
	for _, tc := range []struct {
		what  string
		text  string
		want  int
		title string
	}{
		{"a bare column of addresses", "https://a.example/rss\nhttps://b.example/rss", 2, ""},
		{"a title and address on one line", "The Go Blog — https://go.dev/blog/feed.atom", 1, "The Go Blog"},
		{"a bulleted list", "- Something https://a.example/rss\n- Else https://b.example/rss", 2, "Something"},
		{"addresses in prose", "I read https://a.example/rss most days", 1, "I read"},
	} {
		doc, err := DecodeAny(tc.text)
		if err != nil {
			t.Errorf("%s: %v", tc.what, err)
			continue
		}
		if len(doc.Feeds) != tc.want {
			t.Errorf("%s: %d feeds, want %d (%+v)", tc.what, len(doc.Feeds), tc.want, doc.Feeds)
			continue
		}
		if tc.title != "" && doc.Feeds[0].Title != tc.title {
			t.Errorf("%s: title = %q, want %q", tc.what, doc.Feeds[0].Title, tc.title)
		}
	}
}

// Loose means ignoring what cannot be understood, not choking on it.
func TestIgnoresWhatItCannotUnderstand(t *testing.T) {
	const messy = `Here are my feeds!

The Go Blog
https://go.dev/blog/feed.atom
Engineering

(that one's good)

https://b.example/rss

thanks`

	doc, err := DecodeAny(messy)
	if err != nil {
		t.Fatalf("DecodeAny(): %v", err)
	}
	if len(doc.Feeds) != 2 {
		t.Fatalf("%d feeds, want 2: %+v", len(doc.Feeds), doc.Feeds)
	}
	if doc.Feeds[0].Title != "The Go Blog" {
		t.Errorf("title = %q", doc.Feeds[0].Title)
	}
}

// A list with nothing in it is empty, not an error — "no feeds in that list" is the
// caller's sentence to say.
func TestAListOfNothingIsEmpty(t *testing.T) {
	for _, text := range []string{"", "hello", "no links here\njust words"} {
		doc, err := DecodeAny(text)
		if err != nil {
			t.Errorf("DecodeAny(%q) errored: %v", text, err)
			continue
		}
		if len(doc.Feeds) != 0 {
			t.Errorf("DecodeAny(%q) found %+v", text, doc.Feeds)
		}
	}
}

// XML still goes to the XML reader, and a broken document still says so.
func TestStillTellsTheFormsApart(t *testing.T) {
	const list = `<opml version="2.0"><body>
	  <outline type="rss" text="Feed" xmlUrl="https://example.com/rss"/>
	</body></opml>`

	doc, err := DecodeAny(list)
	if err != nil {
		t.Fatalf("DecodeAny(opml): %v", err)
	}
	if len(doc.Feeds) != 1 {
		t.Fatalf("%d feeds from OPML", len(doc.Feeds))
	}

	if _, err := DecodeAny("<opml><body><outline"); err == nil {
		t.Error("DecodeAny accepted a truncated XML document")
	}
}

// Trailing punctuation belongs to the sentence, not to the address.
func TestTrimsPunctuationFromAnAddress(t *testing.T) {
	doc, _ := DecodeAny("see (https://a.example/rss) for more")
	if len(doc.Feeds) != 1 {
		t.Fatalf("%+v", doc.Feeds)
	}
	if doc.Feeds[0].FeedURL != "https://a.example/rss" {
		t.Errorf("url = %q", doc.Feeds[0].FeedURL)
	}
}
