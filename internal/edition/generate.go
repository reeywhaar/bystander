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

// Generate composes and commits one person's page.
//
// Returns the new edition, or nil when there was nothing to draw from. A principal with no
// feeds — or none that has produced an unshown article — is not an error and does not get
// an empty page: nothing is written, and the caller leaves their clock alone so the first
// real page arrives on the tick after they add a feed rather than a day later.
func (g *Generator) Generate(ctx context.Context, principalID string) (*store.Edition, error) {
	settings, err := g.store.Settings(ctx, principalID)
	if err != nil {
		return nil, err
	}

	subs, err := g.store.ListSubscriptions(ctx, principalID)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}

	tags, err := g.store.ListTags(ctx, principalID)
	if err != nil {
		return nil, err
	}

	feedIDs := make([]string, 0, len(subs))
	for _, sub := range subs {
		feedIDs = append(feedIDs, sub.FeedID)
	}
	// Nothing older than the window this person chose. A front page is about what is
	// going on, and how far back that reaches is theirs to say.
	var notOlderThan time.Time
	if settings.ArticleWindow > 0 {
		notOlderThan = g.store.Now().Add(-settings.ArticleWindow)
	}
	candidates, err := g.store.Candidates(ctx, principalID, feedIDs, candidateDepth, notOlderThan)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// One seed, drawn once and stored with the edition. Drawing it twice would leave the
	// recorded seed unable to reproduce the page it is recorded against, which is the
	// only thing the column is for.
	s := seed()
	buckets, sources := plan(subs, tags, candidates)
	picks := Select(buckets, sources, settings.EditionSize, s)
	if len(picks) == 0 {
		return nil, nil
	}

	ed, err := g.store.ReplaceEdition(ctx, principalID, s, settings.EditionSize, picks)
	if err != nil {
		return nil, err
	}
	g.log.Info("composed a page",
		"principal", principalID, "articles", len(picks), "feeds", len(candidates), "asked_for", settings.EditionSize)
	return ed, nil
}

// plan turns subscriptions and tags into the buckets and sources the sampler works on.
//
// This is where "a bucket is a tag together with the subscriptions tagged directly with
// it" is actually built, including the untagged bucket — which is an ordinary bucket with
// an empty id, not a special case. Tag hierarchy takes no part: parent_id groups tags in
// the manage interface and nothing more.
func plan(subs []*store.Subscription, tags []*store.Tag, candidates map[string][]*store.Item) ([]Bucket, map[string]*Source) {
	sources := make(map[string]*Source, len(subs))
	for _, sub := range subs {
		items := candidates[sub.FeedID]
		if len(items) == 0 {
			continue
		}
		// A feed followed once per principal, so there is no priority to reconcile. If
		// two subscriptions ever pointed at one feed, the schema's unique constraint
		// would have refused the second.
		sources[sub.FeedID] = &Source{FeedID: sub.FeedID, Priority: sub.Priority, Items: items}
	}

	priorities := make(map[string]int, len(tags))
	for _, tag := range tags {
		priorities[tag.ID] = tag.Priority
	}

	byTag := make(map[string][]string)
	var untagged []string
	for _, sub := range subs {
		if sources[sub.FeedID] == nil {
			continue
		}
		if len(sub.TagIDs) == 0 {
			untagged = append(untagged, sub.FeedID)
			continue
		}
		for _, tagID := range sub.TagIDs {
			byTag[tagID] = append(byTag[tagID], sub.FeedID)
		}
	}

	buckets := make([]Bucket, 0, len(byTag)+1)
	// Ordered by the tag list, which is ordered by the store — so the same inputs produce
	// the same bucket order and the seed alone decides the page. Iterating the map would
	// make a generation unreproducible however carefully it was seeded.
	for _, tag := range tags {
		if feeds := byTag[tag.ID]; len(feeds) > 0 {
			buckets = append(buckets, Bucket{TagID: tag.ID, Priority: priorities[tag.ID], FeedIDs: feeds})
		}
	}
	if len(untagged) > 0 {
		buckets = append(buckets, Bucket{TagID: "", Priority: store.DefaultPriority, FeedIDs: untagged})
	}
	return buckets, sources
}

// seed draws a seed for one generation. Random rather than derived from the clock, so two
// people generated in the same second do not get correlated pages.
func seed() int64 {
	var b [8]byte
	rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// GenerateAndSchedule composes a page and moves the clock on.
//
// The clock moves only when a page was actually produced. A principal with no feeds stays
// due, which is what makes their first page arrive on the tick after they add one.
func (g *Generator) GenerateAndSchedule(ctx context.Context, settings *store.Settings, now time.Time) error {
	ed, err := g.Generate(ctx, settings.PrincipalID)
	if err != nil {
		return err
	}
	if ed == nil {
		return nil
	}
	return g.store.ScheduleNextEdition(ctx, settings.PrincipalID, now.Add(settings.EditionInterval))
}

// Regenerate composes a page now, on request, and rebases the clock from this moment so a
// manual regeneration does not leave a stale timer about to fire.
//
// Unlike a scheduled turn, this first returns the current page's unread articles to the
// pool — see store.ReleaseUnread for why. The practical effect is that pressing the button
// twice gives two different arrangements of what your feeds have published, rather than one
// page and then an apology.
func (g *Generator) Regenerate(ctx context.Context, principalID string, now time.Time) (*store.Edition, error) {
	settings, err := g.store.Settings(ctx, principalID)
	if err != nil {
		return nil, err
	}

	released, err := g.store.ReleaseUnread(ctx, principalID)
	if err != nil {
		return nil, err
	}

	ed, err := g.Generate(ctx, principalID)
	if err != nil {
		return nil, err
	}
	if ed != nil && released > 0 {
		g.log.Debug("returned unread articles to the pool before recomposing",
			"principal", principalID, "released", released)
	}
	if ed == nil {
		// Two different answers, and they deserve different words. Somebody with a page on
		// screen being told there is nothing to put on one would reasonably conclude
		// something is broken; what has actually happened is that their feeds have
		// published nothing since the page they are looking at.
		// Everything on the page has been read and the feeds have published nothing
		// since. Unread articles were already returned to the pool above, so there is
		// genuinely nothing left to arrange.
		if _, _, err := g.store.CurrentEdition(ctx, principalID); err == nil {
			return nil, store.Conflict("everything here has been read, and nothing new has been published yet")
		}
		return nil, store.NotFound("there is nothing to put on a page yet — add a feed, and give it a moment to fetch")
	}
	if err := g.store.ScheduleNextEdition(ctx, principalID, now.Add(settings.EditionInterval)); err != nil {
		return nil, err
	}
	return ed, nil
}
