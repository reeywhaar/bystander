package edition

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"log/slog"
	"time"

	"bystander/internal/store"
)

// candidateDepth is how many unshown articles are read per feed before sampling.
//
// The cap means no feed can contribute more than a fifth of a page, so reading much
// deeper than that would be loading rows to throw away. Four times the cap leaves room for
// the sampler to skip articles already drawn through another tag.
const candidateDepth = 60

// Generator composes pages.
type Generator struct {
	store *store.Store
	log   *slog.Logger
}

func NewGenerator(st *store.Store, log *slog.Logger) *Generator {
	return &Generator{store: st, log: log}
}

// Generate composes and commits one page, filling it out with repeats if it has to.
//
// Returns the new edition, or nil when there was nothing to draw from. A page with no feeds —
// or none that has produced an article it can reach — is not an error and does not get an
// empty page: nothing is written, and the caller leaves its clock alone so the first real page
// arrives on the tick after a feed is added rather than a day later.
//
// A page with a filter that matches nothing behaves the same way. That is worth knowing about
// rather than treating as a bug: a page filtered to a tag nobody has used is empty for exactly
// the same reason a new account's is, and telling those two apart is the interface's job.
func (g *Generator) Generate(ctx context.Context, pageID string) (*store.Edition, error) {
	return g.compose(ctx, pageID)
}

// compose does the work behind Generate and Regenerate.
func (g *Generator) compose(ctx context.Context, pageID string) (*store.Edition, error) {
	page, err := g.store.PageByID(ctx, pageID)
	if err != nil {
		return nil, err
	}
	principalID := page.PrincipalID

	subs, err := g.store.ListSubscriptions(ctx, principalID)
	if err != nil {
		return nil, err
	}
	// What this page is allowed to draw from, which for the main page is usually everything.
	subs = eligible(page, subs)
	if len(subs) == 0 {
		return nil, nil
	}

	feedIDs := make([]string, 0, len(subs))
	for _, sub := range subs {
		feedIDs = append(feedIDs, sub.FeedID)
	}
	// Nothing older than each feed's own window. A news feed worth a day and a blog worth
	// a year are exactly the pair one number could not serve, which is why this is per
	// feed rather than per reader.
	//
	// A page may hold a shorter window over the top of it — a finances page wanting only
	// today's news out of feeds that are otherwise worth a week. The tighter of the two wins,
	// because both were asked for and only one of them can be honoured by showing more.
	now := g.store.Now()
	notOlderThan := make(map[string]time.Time, len(subs))
	for _, sub := range subs {
		window := sub.ArticleWindow
		if page.ArticleWindow > 0 && (window == 0 || page.ArticleWindow < window) {
			window = page.ArticleWindow
		}
		if window > 0 {
			notOlderThan[sub.FeedID] = now.Add(-window)
		}
	}
	queues, err := g.store.Queues(ctx, page.ID, principalID, feedIDs, candidateDepth, notOlderThan)
	if err != nil {
		return nil, err
	}
	sources := plan(subs, queues)

	// One seed, drawn once and stored with the edition. Drawing it twice would leave the
	// recorded seed unable to reproduce the page it is recorded against, which is the
	// only thing the column is for.
	s := seed()
	picks := Select(sources, page.EditionSize, s)
	if len(picks) == 0 {
		return nil, nil
	}

	ed, err := g.store.AddEdition(ctx, page, s, picks)
	if err != nil {
		return nil, err
	}
	g.log.Info("composed a page", "page", page.ID, "principal", principalID,
		"articles", len(picks), "feeds", len(sources), "asked_for", page.EditionSize)
	return ed, nil
}

// eligible is the subscriptions one page may draw from.
//
// The tags are a funnel and the feeds override what comes out of it, so the feeds are settled
// first — an override that ran second would still be an override, but reading it that way makes
// the ordering look like a detail rather than the meaning.
//
// **A feed the page has an opinion about takes it, whatever the tags say.** On means on and off
// means off. Those are the two gestures anybody actually makes about one publisher — "this one
// as well" and "this one never" — and neither was sayable when the feed list was a second
// funnel narrowing the first.
//
// **Otherwise the tags decide.** Any tag on the include side holds the page to subscriptions
// carrying at least one of them; an empty include side is not a filter, it means the page was
// never narrowed this way rather than narrowed to nothing. Then the exclude side drops what it
// matches — after, and that ordering is the whole point of having both. Tags overlap, and
// "Finance, but not the feeds that are also Crypto" needs the include to have happened first.
func eligible(page *store.Page, subs []*store.Subscription) []*store.Subscription {
	always := set(page.IncludeFeedIDs)
	never := set(page.ExcludeFeedIDs)
	include := set(page.IncludeTagIDs)
	exclude := set(page.ExcludeTagIDs)

	out := make([]*store.Subscription, 0, len(subs))
	for _, sub := range subs {
		switch {
		case never[sub.FeedID]:
			continue
		case always[sub.FeedID]:
		case len(include) > 0 && !anyOf(sub.TagIDs, include):
			continue
		case anyOf(sub.TagIDs, exclude):
			continue
		}
		out = append(out, sub)
	}
	return out
}

func set(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func anyOf(ids []string, wanted map[string]bool) bool {
	for _, id := range ids {
		if wanted[id] {
			return true
		}
	}
	return false
}

// plan pairs each subscription with what its feed can offer this page.
//
// All this does now is join two maps, and it used to build tag buckets as well. Tags decide
// whether a feed is on the page at all — that is eligible above, and the page's own filter
// lists — and once that is settled they weigh nothing. A tag priority on top of it meant a
// feed carrying three tags sat in three buckets and took a quarter of the page at the same
// slider setting as a feed carrying one, which is not a thing anybody asked for.
func plan(subs []*store.Subscription, queues map[string]*store.Queue) map[string]*Source {
	sources := make(map[string]*Source, len(subs))
	for _, sub := range subs {
		q := queues[sub.FeedID]
		if q == nil {
			continue
		}
		// A feed is followed once per principal, so there is no priority to reconcile. If
		// two subscriptions ever pointed at one feed, the schema's unique constraint would
		// have refused the second.
		sources[sub.FeedID] = &Source{
			FeedID:   sub.FeedID,
			Priority: sub.Priority,
			Fresh:    q.Fresh,
			Unread:   q.Unread,
			Read:     q.Read,
		}
	}
	return sources
}

// seed draws a seed for one generation. Random rather than derived from the clock, so two
// people generated in the same second do not get correlated pages.
func seed() int64 {
	var b [8]byte
	rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// GenerateAndSchedule composes a page and moves its clock on.
//
// The clock moves only when a page was actually produced. A page with nothing to draw from
// stays due, which is what makes its first edition arrive on the tick after a feed is added.
func (g *Generator) GenerateAndSchedule(ctx context.Context, page *store.Page, now time.Time) error {
	ed, err := g.Generate(ctx, page.ID)
	if err != nil {
		return err
	}
	if ed == nil {
		return nil
	}
	return g.store.ScheduleNextEdition(ctx, page.ID, now.Add(page.EditionInterval))
}

// Regenerate composes a page now, on request, and rebases the clock from this moment so a
// manual regeneration does not leave a stale timer about to fire.
//
// Unlike a scheduled turn, this first returns the current page's unread articles to the
// pool — see store.ReleaseUnread for why. The practical effect is that pressing the button
// twice gives two different arrangements of what your feeds have published, rather than one
// page and then an apology.
func (g *Generator) Regenerate(ctx context.Context, pageID string, now time.Time) (*store.Edition, error) {
	page, err := g.store.PageByID(ctx, pageID)
	if err != nil {
		return nil, err
	}

	released, err := g.store.ReleaseUnread(ctx, pageID)
	if err != nil {
		return nil, err
	}

	// Composed out of whatever there is, exactly like a scheduled turn. It used to refuse
	// when nothing was fresh, on the reasoning that handing back the same articles greyed is
	// not a different page — but a page is an arrangement as much as a set. A new seed draws
	// a different subset, in a different order, into different slots, and that is what the
	// button is for. Refusing left somebody who had read everything with no way to do
	// anything at all until a publisher posted.
	ed, err := g.compose(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if ed != nil && released > 0 {
		g.log.Debug("returned unread articles to the pool before recomposing",
			"page", pageID, "released", released)
	}
	if ed == nil {
		// The only way here now: no feed this page can draw from has produced a single
		// article it can reach. Not "everything is read" — read articles are drawn like
		// anything else, just last.
		return nil, store.NotFound("there is nothing to put on a page yet — add a feed, and give it a moment to fetch")
	}
	if err := g.store.ScheduleNextEdition(ctx, pageID, now.Add(page.EditionInterval)); err != nil {
		return nil, err
	}
	return ed, nil
}
