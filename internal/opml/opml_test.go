package opml

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	original := Document{
		Title:     "reey's feeds",
		OwnerName: "reey",
		CreatedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		Feeds: []Feed{
			{
				Title:      "The Go Blog",
				FeedURL:    "https://go.dev/blog/feed.atom",
				Categories: [][]string{{"Engineering"}},
				Priority:   90,
			},
			{
				Title:      "Hi-Fructose",
				FeedURL:    "https://hifructose.com/feed/",
				SiteURL:    "https://hifructose.com",
				Categories: [][]string{{"Art"}, {"News", "World"}},
				Priority:   50,
			},
		},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, original); err != nil {
		t.Fatalf("Encode(): %v", err)
	}

	// GMT, as in every date in the spec's examples — not "UTC", which is what
	// time.RFC1123 would write for the same instant.
	if !strings.Contains(buf.String(), "Sun, 23 Aug 2026 12:00:00 GMT") {
		t.Errorf("dateCreated is not in the spec's form:\n%s", buf.String())
	}

	// Flat, deliberately: several tags per feed is the thing nesting cannot express.
	if strings.Count(buf.String(), "<outline") != 2 {
		t.Errorf("wrote %d outlines for 2 feeds; the list should be flat:\n%s",
			strings.Count(buf.String(), "<outline"), buf.String())
	}

	back, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if back.Title != original.Title || back.OwnerName != original.OwnerName {
		t.Errorf("head = %+v", back)
	}
	if len(back.Feeds) != 2 {
		t.Fatalf("%d feeds came back, want 2", len(back.Feeds))
	}

	for i, want := range original.Feeds {
		got := back.Feeds[i]
		if got.Title != want.Title || got.FeedURL != want.FeedURL || got.SiteURL != want.SiteURL {
			t.Errorf("feed %d = %+v, want %+v", i, got, want)
		}
		if got.Priority != want.Priority {
			t.Errorf("feed %d priority = %d, want %d", i, got.Priority, want.Priority)
		}
		if len(got.Categories) != len(want.Categories) {
			t.Fatalf("feed %d has %v, want %v", i, got.Categories, want.Categories)
		}
		for j := range want.Categories {
			if strings.Join(got.Categories[j], "/") != strings.Join(want.Categories[j], "/") {
				t.Errorf("feed %d category %d = %v, want %v", i, j, got.Categories[j], want.Categories[j])
			}
		}
	}
}

// The spec's own example of the attribute, so the encoding is checked against the format
// rather than against itself.
func TestCategoryMatchesTheSpecsExample(t *testing.T) {
	const want = "/Philosophy/Baseball/Mets,/Tourism/New York"
	got := EncodeCategories([][]string{
		{"Philosophy", "Baseball", "Mets"},
		{"Tourism", "New York"},
	})
	if got != want {
		t.Errorf("EncodeCategories() = %q, want %q", got, want)
	}

	paths := ParseCategories(want)
	if len(paths) != 2 || paths[0][2] != "Mets" || paths[1][1] != "New York" {
		t.Errorf("ParseCategories(%q) = %v", want, paths)
	}
}

// OPML gives no escaping rule, so a tag with a comma or a slash in it would quietly split
// into two. It has to survive instead.
func TestPunctuationInATagSurvives(t *testing.T) {
	for _, name := range []string{"Art, Design", "TV/Film", "100% Cotton", "Comma,Slash/Both"} {
		encoded := EncodeCategories([][]string{{name}})
		if strings.Count(encoded, ",") != 0 || strings.Count(encoded, "/") != 1 {
			t.Errorf("EncodeCategories(%q) = %q, which would be read as more than one tag", name, encoded)
		}
		back := ParseCategories(encoded)
		if len(back) != 1 || len(back[0]) != 1 || back[0][0] != name {
			t.Errorf("%q survived as %v", name, back)
		}
	}
}

// A file from any other reader will use folders. Refusing it because we would have written
// it differently is a poor way to meet somebody's subscriptions.
func TestReadsNestedListsFromOtherReaders(t *testing.T) {
	const nested = `<?xml version="1.0"?>
<opml version="1.0">
  <head><title>subscriptions</title></head>
  <body>
    <outline text="News">
      <outline text="World">
        <outline type="rss" text="Reuters" xmlUrl="https://reuters.com/rss"/>
      </outline>
      <outline type="rss" text="Local" xmlUrl="https://local.example/rss"/>
    </outline>
    <outline type="rss" text="Loose" xmlUrl="https://loose.example/rss"/>
  </body>
</opml>`

	doc, err := Decode(strings.NewReader(nested))
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(doc.Feeds) != 3 {
		t.Fatalf("%d feeds, want 3", len(doc.Feeds))
	}

	byTitle := map[string][][]string{}
	for _, feed := range doc.Feeds {
		byTitle[feed.Title] = feed.Categories
	}
	if got := byTitle["Reuters"]; len(got) != 1 || strings.Join(got[0], "/") != "News/World" {
		t.Errorf("Reuters is filed under %v, want News/World", got)
	}
	if got := byTitle["Local"]; len(got) != 1 || strings.Join(got[0], "/") != "News" {
		t.Errorf("Local is filed under %v, want News", got)
	}
	if got := byTitle["Loose"]; len(got) != 0 {
		t.Errorf("Loose is filed under %v, want nothing", got)
	}
}

// An explicit category can say more than one path; folders cannot. When a file carries
// both, the attribute is the one that was not forced to choose.
func TestAnExplicitCategoryBeatsTheFolderItSitsIn(t *testing.T) {
	const both = `<opml version="2.0"><body>
	  <outline text="Folder">
	    <outline type="rss" text="Feed" xmlUrl="https://example.com/rss" category="/Art,/Design"/>
	  </outline>
	</body></opml>`

	doc, err := Decode(strings.NewReader(both))
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(doc.Feeds) != 1 {
		t.Fatalf("%d feeds, want 1", len(doc.Feeds))
	}
	if len(doc.Feeds[0].Categories) != 2 {
		t.Errorf("categories = %v, want the two the attribute named", doc.Feeds[0].Categories)
	}
}

// An outline with no xmlUrl is a folder, a comment, or somebody's notes — never a
// subscription.
func TestOutlinesWithNoFeedAreNotFeeds(t *testing.T) {
	const mixed = `<opml version="2.0"><body>
	  <outline text="just a note"/>
	  <outline text="a link" type="link" url="https://example.com"/>
	  <outline text="Real" type="rss" xmlUrl="https://example.com/rss"/>
	</body></opml>`

	doc, err := Decode(strings.NewReader(mixed))
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(doc.Feeds) != 1 || doc.Feeds[0].Title != "Real" {
		t.Errorf("got %+v, want only the one with an xmlUrl", doc.Feeds)
	}
}

// A file that never mentions priority must not silently import everything at zero, which
// would mean subscribing to a list that can never appear.
func TestAMissingPriorityIsNotZero(t *testing.T) {
	const plain = `<opml version="2.0"><body>
	  <outline type="rss" text="Feed" xmlUrl="https://example.com/rss"/>
	</body></opml>`

	doc, err := Decode(strings.NewReader(plain))
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if doc.Feeds[0].Priority != -1 {
		t.Errorf("priority = %d, want -1 for absent", doc.Feeds[0].Priority)
	}
}

func TestRejectsWhatIsNotOpml(t *testing.T) {
	for _, bad := range []string{"", "not xml at all", "<html><body>hi</body></html>"} {
		if _, err := Decode(strings.NewReader(bad)); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}
