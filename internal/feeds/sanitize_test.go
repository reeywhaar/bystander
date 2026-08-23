package feeds

import (
	"strings"
	"testing"
)

func TestSanitizeDropsScripts(t *testing.T) {
	got := Sanitize(`<p>Before</p><script>alert(1)</script><p>After</p>`, "https://example.com/feed")
	if strings.Contains(got, "alert") {
		t.Errorf("script contents survived: %q", got)
	}
	if !strings.Contains(got, "Before") || !strings.Contains(got, "After") {
		t.Errorf("prose around the script was lost: %q", got)
	}
}

// Unknown tags are unwrapped rather than dropped: a publisher wrapping a paragraph in a
// div should not cost the paragraph.
func TestSanitizeUnwrapsUnknownTags(t *testing.T) {
	got := Sanitize(`<div class="x"><p>Kept</p></div>`, "https://example.com/feed")
	if got != "<p>Kept</p>" {
		t.Errorf("Sanitize() = %q, want %q", got, "<p>Kept</p>")
	}
}

func TestSanitizeStripsAttributes(t *testing.T) {
	got := Sanitize(`<p onclick="steal()" style="color:red">Hello</p>`, "https://example.com/feed")
	if got != "<p>Hello</p>" {
		t.Errorf("Sanitize() = %q, want %q", got, "<p>Hello</p>")
	}
}

func TestSanitizeRefusesDangerousSchemes(t *testing.T) {
	for _, href := range []string{"javascript:alert(1)", "data:text/html,<b>x", "vbscript:x"} {
		got := Sanitize(`<a href="`+href+`">click</a>`, "https://example.com/feed")
		if strings.Contains(got, "href") {
			t.Errorf("Sanitize(%q) kept an href: %q", href, got)
		}
		// The words survive; only the link goes.
		if !strings.Contains(got, "click") {
			t.Errorf("Sanitize(%q) lost the link text: %q", href, got)
		}
	}
}

func TestSanitizeResolvesRelativeLinks(t *testing.T) {
	got := Sanitize(`<a href="/story">read</a>`, "https://example.com/feed.xml")
	if !strings.Contains(got, `href="https://example.com/story"`) {
		t.Errorf("Sanitize() = %q, want an absolute href", got)
	}
	if !strings.Contains(got, `rel="noopener noreferrer"`) {
		t.Errorf("Sanitize() = %q, want rel on an outbound link", got)
	}
}

// However the input ended, and wherever truncation lands, the result must be balanced —
// an unclosed tag would leak into the rest of the page.
func TestSanitizeBalancesTags(t *testing.T) {
	for _, in := range []string{
		`<p>unclosed`,
		`<p><em>both unclosed`,
		`<p>fine</p></div>`,
		`<b>a</i>b</b>`,
	} {
		got := Sanitize(in, "https://example.com/")
		if strings.Count(got, "<") != strings.Count(got, ">") {
			t.Errorf("Sanitize(%q) = %q: malformed", in, got)
		}
		if opens, closes := strings.Count(got, "<p"), strings.Count(got, "</p>"); opens != closes {
			t.Errorf("Sanitize(%q) = %q: %d <p> against %d </p>", in, got, opens, closes)
		}
	}
}

func TestSanitizeTruncates(t *testing.T) {
	long := strings.Repeat("word ", 400)
	got := Sanitize("<p>"+long+"</p>", "https://example.com/")

	if len([]rune(Text(got))) > MaxSummaryRunes+2 {
		t.Errorf("Sanitize() kept %d runes, want about %d", len([]rune(Text(got))), MaxSummaryRunes)
	}
	if !strings.HasSuffix(got, "</p>") {
		t.Errorf("truncation left the markup unbalanced: %q", got[max(0, len(got)-40):])
	}
	if !strings.Contains(got, "…") {
		t.Error("truncation did not mark that something was cut")
	}
}

func TestSanitizeEscapesText(t *testing.T) {
	got := Sanitize(`<p>5 &lt; 6 &amp; 7 &gt; 6</p>`, "https://example.com/")
	if strings.Contains(got, "<script") {
		t.Fatalf("Sanitize() = %q", got)
	}
	// Entities are decoded by the tokenizer and must be re-escaped, not passed through raw.
	if !strings.Contains(got, "&lt;") || !strings.Contains(got, "&amp;") {
		t.Errorf("Sanitize() = %q, want the entities re-escaped", got)
	}
}

func TestSanitizeEmpty(t *testing.T) {
	if got := Sanitize("   ", "https://example.com/"); got != "" {
		t.Errorf("Sanitize(blank) = %q, want empty", got)
	}
}

func TestText(t *testing.T) {
	if got := Text(`<p>Hello   <em>there</em></p>`); got != "Hello there" {
		t.Errorf("Text() = %q, want %q", got, "Hello there")
	}
}
