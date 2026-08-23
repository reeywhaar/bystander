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
	due, err := s.store.DueSettings(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("could not find out whose page is due", "error", err)
		}
		return
	}
	now := s.store.Now()
	for _, settings := range due {
		if ctx.Err() != nil {
			return
		}
		// One principal's failure is not the others'. A feed that produced something
		// unparseable should not stop everybody else's page being composed.
		if err := s.gen.GenerateAndSchedule(ctx, settings, now); err != nil {
			s.log.Error("could not compose a page", "principal", settings.PrincipalID, "error", err)
		}
	}
}

// sweep collects what has expired. Everything it touches is in derived.db, and everything
// it touches is reconstructible — which is why it can afford to be this blunt.
func (s *Scheduler) sweep(ctx context.Context) {
	principals, err := s.store.ListPrincipals(ctx)
	if err != nil {
		s.log.Error("could not list accounts for the sweep", "error", err)
		return
	}
	ids := make([]string, 0, len(principals))
	for _, p := range principals {
		ids = append(ids, p.ID)
	}
	if n, err := s.store.DeleteEditionsExcept(ctx, ids); err != nil {
		s.log.Error("could not collect the pages of deleted accounts", "error", err)
	} else if n > 0 {
		s.log.Info("collected pages belonging to deleted accounts", "count", n)
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
	if n, err := s.store.PruneItems(ctx, feedIDs); err != nil {
		s.log.Error("could not prune articles", "error", err)
	} else if n > 0 {
		s.log.Info("pruned articles", "count", n)
	}

	if n, err := s.store.PruneShown(ctx); err != nil {
		s.log.Error("could not prune the record of what has been shown", "error", err)
	} else if n > 0 {
		s.log.Debug("pruned shown records", "count", n)
	}

	if n, err := s.store.PruneReadArticles(ctx); err != nil {
		s.log.Error("could not prune what was read", "error", err)
	} else if n > 0 {
		s.log.Debug("pruned read articles past retention", "count", n)
	}
}
