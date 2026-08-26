package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// exported is the document inside the archive, as a test reads it back.
type exported struct {
	Format     string                  `json:"format"`
	Version    int                     `json:"version"`
	ExportedAt int64                   `json:"exported_at"`
	Instance   string                  `json:"instance"`
	Account    store.ExportedAccount   `json:"account"`
	Tags       []store.ExportedTag     `json:"tags"`
	Feeds      []store.ExportedFeed    `json:"feeds"`
	Pages      []store.ExportedPage    `json:"pages"`
	Read       []store.ExportedArticle `json:"read"`
	Unread     []store.ExportedArticle `json:"unread"`
}

// download fetches the archive, opens it, and hands back the document and its filename.
func (h *harness) download(t *testing.T) (exported, string) {
	t.Helper()

	res := h.do(http.MethodGet, "/api/account/export", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("export answered %d: %s", res.StatusCode, raw)
	}
	if got := res.Header.Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", got)
	}

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	// Opened as a zip rather than trusted to be one. The central directory is written
	// last, so a reader that opens is a reader that received the whole thing.
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != ExportEntry {
		names := []string{}
		for _, f := range archive.File {
			names = append(names, f.Name)
		}
		t.Fatalf("archive holds %v, want just %s", names, ExportEntry)
	}

	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatalf("open %s: %v", ExportEntry, err)
	}
	defer entry.Close()

	var doc exported
	if err := json.NewDecoder(entry).Decode(&doc); err != nil {
		t.Fatalf("decode %s: %v", ExportEntry, err)
	}
	return doc, res.Header.Get("Content-Disposition")
}

func TestExportCarriesEverythingAccountHolds(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	// A tag, a feed filed under it, and something read.
	var tag tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Science", "priority": 70}),
		http.StatusCreated, &tag)

	me := h.me(t)
	feed, err := h.store.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Meridian", "https://example.com")
	if err != nil {
		t.Fatalf("UpsertFeed(): %v", err)
	}
	if _, err := h.store.Subscribe(t.Context(), me, feed.ID, 80, 7*24*time.Hour, []string{tag.ID}); err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	// Two articles, one of them read, so both long sections have something in them.
	items := []*store.Item{
		{FeedID: feed.ID, GUID: "one", Title: "Bells", Link: "https://example.com/1",
			PublishedAt: h.store.Now().Add(-time.Hour), FetchedAt: h.store.Now()},
		{FeedID: feed.ID, GUID: "two", Title: "Whistles", Link: "https://example.com/2",
			PublishedAt: h.store.Now().Add(-2 * time.Hour), FetchedAt: h.store.Now()},
	}
	if _, err := h.store.SaveItems(t.Context(), items); err != nil {
		t.Fatalf("SaveItems(): %v", err)
	}
	// SaveItems names each article itself — an id is derived from the feed and the guid —
	// so the id to mark read is the one it wrote back, not one a test made up.
	if err := h.store.SetRead(t.Context(), me, items[0].ID, true); err != nil {
		t.Fatalf("SetRead(): %v", err)
	}

	doc, disposition := h.download(t)

	if doc.Format != ExportFormat || doc.Version != ExportVersion {
		t.Errorf("format = %q v%d, want %q v%d", doc.Format, doc.Version, ExportFormat, ExportVersion)
	}
	if doc.ExportedAt == 0 {
		t.Error("the export does not say when it was taken")
	}
	if doc.Instance == "" {
		t.Error("the export does not say where it came from")
	}
	if doc.Account.Username != "ada" {
		t.Errorf("account.username = %q, want ada", doc.Account.Username)
	}

	if len(doc.Tags) != 1 || doc.Tags[0].Name != "Science" || doc.Tags[0].Priority != 70 {
		t.Errorf("tags = %+v", doc.Tags)
	}
	if len(doc.Feeds) != 1 {
		t.Fatalf("feeds = %+v, want one", doc.Feeds)
	}
	if doc.Feeds[0].Priority != 80 {
		t.Errorf("the feed's priority did not survive: %+v", doc.Feeds[0])
	}
	if len(doc.Feeds[0].Tags) != 1 || doc.Feeds[0].Tags[0] != "Science" {
		t.Errorf("feed tags = %v, want [Science] — named, not referenced", doc.Feeds[0].Tags)
	}
	// Every account has a main page from the moment it exists.
	if len(doc.Pages) == 0 {
		t.Error("no pages were exported")
	}

	// What was read, and what is still waiting — each naming its feed by URL rather than
	// by an id that means nothing outside this instance.
	if len(doc.Read) != 1 || doc.Read[0].Title != "Bells" {
		t.Fatalf("read = %+v, want one article", doc.Read)
	}
	if doc.Read[0].ReadAt == 0 {
		t.Error("a read article does not say when it was read")
	}
	if doc.Read[0].FeedURL == "" || doc.Read[0].FeedTitle != "The Meridian" {
		t.Errorf("a read article does not name its feed: %+v", doc.Read[0])
	}
	if len(doc.Unread) != 1 || doc.Unread[0].Title != "Whistles" {
		t.Fatalf("unread = %+v, want the one that was not read", doc.Unread)
	}
	if doc.Unread[0].ReadAt != 0 {
		t.Error("an unread article carries a read time")
	}

	// A filename somebody can find again, carrying the username and the date.
	if !strings.Contains(disposition, "ada") || !strings.HasSuffix(disposition, `.zip"`) {
		t.Errorf("Content-Disposition = %q", disposition)
	}
}

// The archive holds nothing that could be replayed against the instance, and nothing that
// is a liability to whoever ends up holding the file.
func TestExportCarriesNothingSecret(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	res := h.do(http.MethodGet, "/api/account/export", nil)
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(entry)
	entry.Close()
	if err != nil {
		t.Fatal(err)
	}

	for _, forbidden := range []string{"password", "$2a$", "id_hash", "token"} {
		if bytes.Contains(bytes.ToLower(body), []byte(forbidden)) {
			t.Errorf("the export mentions %q", forbidden)
		}
	}
}

// Somebody else's data is not in your archive, whoever asks.
func TestExportIsOnlyYourOwn(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	var tag tagBody
	h.expect(h.do(http.MethodPost, "/api/tags", map[string]any{"name": "Mine"}),
		http.StatusCreated, &tag)

	bob := h.signInAsSomebodyElse("bob")
	res := h.doAs(bob, http.MethodGet, "/api/account/export", nil)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()

	if bytes.Contains(raw, []byte("Mine")) {
		t.Error("one account's export carries another's tag")
	}
}

func TestExportNeedsASession(t *testing.T) {
	h := newHarness(t)
	h.expect(h.do(http.MethodGet, "/api/account/export", nil), http.StatusUnauthorized, nil)
}

// Empty arrays rather than null. A field that is sometimes a list and sometimes nothing is a
// field every reader of the file has to special-case.
func TestExportUsesEmptyArraysNotNull(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	res := h.do(http.MethodGet, "/api/account/export", nil)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()

	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := archive.File[0].Open()
	body, _ := io.ReadAll(entry)
	entry.Close()

	if bytes.Contains(body, []byte("null")) {
		t.Errorf("the export contains null:\n%s", body)
	}
}

func TestSafeName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"ada", "ada"},
		{"ada.lovelace", "ada-lovelace"},
		// A name that reached the header unchanged could steer a browser's path.
		{`../../etc/passwd`, "------etc-passwd"},
		{`a"b`, "a-b"},
		{"", "account"},
	} {
		if got := safeName(c.in); got != c.want {
			t.Errorf("safeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The two long sections are read in batches while the archive is being written, so the seam
// between batches is where an export would lose or repeat rows. Two and a half batches of
// read articles, checked for both.
func TestExportPagesThroughManyArticles(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	me := h.me(t)
	feed, err := h.store.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Meridian", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Subscribe(t.Context(), me, feed.ID, 50, 7*24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}

	const total = store.ExportBatch*2 + 37
	items := make([]*store.Item, 0, total)
	for i := range total {
		items = append(items, &store.Item{
			ID:     "a_" + strconv.Itoa(i),
			FeedID: feed.ID,
			GUID:   strconv.Itoa(i),
			Title:  "Article " + strconv.Itoa(i),
			Link:   "https://example.com/" + strconv.Itoa(i),
			// Deliberately colliding timestamps: the cursor's first half is not unique,
			// which is exactly why it has a second half. A whole batch could share one
			// second, and a cursor of the timestamp alone would then skip or repeat.
			PublishedAt: h.store.Now().Add(-time.Duration(i/50) * time.Minute),
			FetchedAt:   h.store.Now(),
		})
	}
	if _, err := h.store.SaveItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if err := h.store.SetRead(t.Context(), me, item.ID, true); err != nil {
			t.Fatal(err)
		}
	}

	doc, _ := h.download(t)

	if len(doc.Read) != total {
		t.Fatalf("read = %d articles, want %d", len(doc.Read), total)
	}
	seen := map[string]bool{}
	for _, article := range doc.Read {
		if seen[article.Link] {
			t.Fatalf("%s appears twice — a batch seam repeated a row", article.Link)
		}
		seen[article.Link] = true
	}
	if len(doc.Unread) != 0 {
		t.Errorf("unread = %d, want none: everything was read", len(doc.Unread))
	}
}
