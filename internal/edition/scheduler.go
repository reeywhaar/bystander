package edition

import (
	"context"
	"log/slog"
	"time"

	"bystander/internal/store"
)

// tick is how often the scheduler looks for work. A minute, because the shortest interval
// on offer is an hour and being up to a minute late on that is not something anybody can
// perceive.
const tick = time.Minute

// sweepInterval is how often derived data is collected: items past retention, the record
// of what was shown, and the pages of accounts that no longer exist.
const sweepInterval = time.Hour

// Scheduler generates pages when they come due, and collects what has expired.
type Scheduler struct {
	store *store.Store
	gen   *Generator
	log   *slog.Logger
}

func NewScheduler(st *store.Store, gen *Generator, log *slog.Logger) *Scheduler {
	return &Scheduler{store: st, gen: gen, log: log}
}

// Run works until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	pages := time.NewTicker(tick)
	defer pages.Stop()
	sweep := time.NewTicker(sweepInterval)
	defer sweep.Stop()

	// Once at startup, so a page that came due while the service was down is generated on
	// boot rather than up to a minute later.
	s.generateDue(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-pages.C:
			s.generateDue(ctx)
		case <-sweep.C:
			s.sweep(ctx)
		}
	}
}

func (s *Scheduler) generateDue(ctx context.Context) {
	due, err := s.store.DuePages(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("could not find out which pages are due", "error", err)
		}
		return
	}
	now := s.store.Now()
	for _, page := range due {
		if ctx.Err() != nil {
			return
		}
		// One page's failure is not the others'. A feed that produced something unparseable
		// should not stop anybody else's page being composed — nor this person's other pages,
		// which is newly true and worth the same care.
		if err := s.gen.GenerateAndSchedule(ctx, page, now); err != nil {
			s.log.Error("could not compose a page",
				"page", page.ID, "principal", page.PrincipalID, "error", err)
		}
	}
}

// sweep collects what has expired. Everything it touches is in derived.db, and everything
// it touches is reconstructible — which is why it can afford to be this blunt.
func (s *Scheduler) sweep(ctx context.Context) {
	// Accounts whose week has run out, first — before anything below reads the lists of
	// what is live. This is the one part of the sweep that is not housekeeping: it is the
	// second half of somebody asking to be erased, and it is here rather than on a clock of
	// its own because erasing the principal is only half of it. Everything in main.db goes
	// by cascade; the editions, what was shown and what was read live in the other database
	// and are collected by the passes below, in this same run.
	if erased, err := s.store.PurgeDeletedAccounts(ctx, store.DeletionGrace); err != nil {
		s.log.Error("could not erase accounts that asked to be", "error", err)
	} else {
		for _, account := range erased {
			// Named, and at Info. After this there is nothing left to look either the id
			// or the name up in, and an erasure that leaves no trace anywhere is
			// indistinguishable from a bug that lost somebody's account.
			s.log.Info("erased an account that asked to be",
				"principal", account.ID, "username", account.Username)
		}
	}

	// Pages rather than accounts, which covers both: a deleted account takes its pages with
	// it by cascade, and a page deleted on its own leaves an edition behind that nothing else
	// would collect.
	live, err := s.store.LivePageIDs(ctx)
	if err != nil {
		s.log.Error("could not list pages for the sweep", "error", err)
		return
	}
	principals, err := s.store.ListPrincipals(ctx)
	if err != nil {
		s.log.Error("could not list accounts for the sweep", "error", err)
		return
	}
	people := make([]string, 0, len(principals))
	for _, p := range principals {
		people = append(people, p.ID)
	}
	// Editions a page has moved on from. They pile up because composing does not delete, so
	// something has to, and this is the something.
	if n, err := s.store.PruneOldEditions(ctx); err != nil {
		s.log.Error("could not collect superseded editions", "error", err)
	} else if n > 0 {
		s.log.Debug("collected superseded editions", "count", n)
	}

	if n, err := s.store.DeleteEditionsExcept(ctx, live, people); err != nil {
		s.log.Error("could not collect the editions of pages that are gone", "error", err)
	} else if n > 0 {
		s.log.Info("collected editions of pages that are gone", "count", n)
	}

	// Feeds nobody follows go first, so the item sweep below sees the shorter list and
	// collects their articles in the same pass.
	if n, err := s.store.DeleteOrphanFeeds(ctx); err != nil {
		s.log.Error("could not collect unfollowed feeds", "error", err)
	} else if n > 0 {
		s.log.Info("collected feeds nobody follows", "count", n)
	}

	feedIDs, err := s.store.FeedIDs(ctx)
	if err != nil {
		s.log.Error("could not list feeds for the sweep", "error", err)
		return
	}

	// How long a feed's articles are kept follows the people who follow *that feed*. One
	// number for the instance was one too few: it took the longest window chosen anywhere,
	// so a webcomic somebody wanted a year of made a news feed at ninety articles a day
	// keep a year as well.
	perFeed, err := s.store.ItemRetentionByFeed(ctx)
	if err != nil {
		s.log.Error("could not work out how long to keep each feed's articles", "error", err)
		return
	}

	if n, err := s.store.PruneItems(ctx, perFeed); err != nil {
		s.log.Error("could not prune articles", "error", err)
	} else if n > 0 {
		s.log.Info("pruned articles", "count", n)
	}

	// The backstop under that rule, for the feed it cannot bound on its own: long-windowed
	// and very high volume. Named per feed rather than counted in total, because a feed
	// turning up here is a feed whose window is being shortened by something other than the
	// setting somebody chose, and that should not happen quietly.
	if cut, err := s.store.CapItemsPerFeed(ctx, feedIDs, store.MaxItemsPerFeed); err != nil {
		s.log.Error("could not hold feeds to the article ceiling", "error", err)
	} else {
		for feedID, n := range cut {
			s.log.Info("a feed reached the article ceiling, so its oldest were dropped",
				"feed", feedID, "count", n, "ceiling", store.MaxItemsPerFeed)
		}
	}

	// Held to a count rather than to a date — see PruneShown, which needs nothing passed in
	// because a page's memory of a feed is bounded by what that feed can hold.
	if n, err := s.store.PruneShown(ctx); err != nil {
		s.log.Error("could not prune the record of what has been shown", "error", err)
	} else if n > 0 {
		s.log.Debug("pruned shown records", "count", n)
	}

	// What was read on feeds that are gone. Nothing here goes by age: what somebody has read
	// is kept for as long as they follow the feed, because it is what keeps an article they
	// have finished with off their pages.
	if n, err := s.store.PruneReadArticles(ctx, feedIDs); err != nil {
		s.log.Error("could not prune what was read", "error", err)
	} else if n > 0 {
		s.log.Debug("pruned what was read on feeds that are gone", "count", n)
	}

	// Shared links are checked for expiry when they are opened, so this is housekeeping
	// rather than enforcement: it stops a list of what somebody reads sitting in the
	// database for months after the week it was good for.
	if n, err := s.store.PruneShares(ctx); err != nil {
		s.log.Error("could not prune expired share links", "error", err)
	} else if n > 0 {
		s.log.Debug("pruned expired share links", "count", n)
	}
}
