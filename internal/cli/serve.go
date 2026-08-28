package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"bystander/internal/api"
	"bystander/internal/app"
	"bystander/internal/config"
	"bystander/internal/edition"
	"bystander/internal/feeds"
	"bystander/internal/jobs"
	"bystander/internal/session"
	"bystander/internal/store"
)

// shutdownGrace is how long in-flight requests have to finish once a signal arrives.
const shutdownGrace = 10 * time.Second

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the reader",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cmd.Context())
		},
	}
}

func serve(parent context.Context) error {
	cfg, st, log, err := setup()
	if err != nil {
		return err
	}
	defer st.Close()

	if main, derived, err := st.SchemaVersions(parent); err == nil {
		log.Info("databases open", "dir", cfg.DataDir, "main_schema", main, "derived_schema", derived)
	}

	if cfg.InsecurePublicURL() {
		// Legitimate behind a terminating proxy on a private network, and a real mistake
		// anywhere else. Warning rather than refusing, because we cannot tell which.
		log.Warn("the public URL is plain http, so the session cookie will not be marked Secure",
			"url", cfg.PublicURL.String(), "variable", config.PublicURLEnv)
	}

	if err := bootstrap(parent, cfg, st, log); err != nil {
		return err
	}

	// os.DirFS rather than an embed. The bundle is a directory beside the binary, so a
	// change to it is not a change to anything the Go compiler has seen — see
	// config.DefaultWebDir. NewSPA reads the whole of it into memory here and never looks
	// at the directory again, so a missing one is the same as an empty one: the placeholder.
	spa, err := api.NewSPA(os.DirFS(cfg.WebDir), log)
	if err != nil {
		return fmt.Errorf("load the frontend from %s: %w", cfg.WebDir, err)
	}

	sessions := session.New(st, cfg.Secure, log)
	fetcher := feeds.NewFetcher(cfg.PublicURL.String())
	generator := edition.NewGenerator(st, log)
	scheduler := edition.NewScheduler(st, generator, log)

	// Everything this program does when nobody is looking at it, registered in one place so
	// that one file lists it.
	//
	// Both kinds are outbound requests to other people's servers and they want opposite things,
	// which is why the pace is per kind rather than shared. Fetching feeds used to be a poller
	// of its own — its own ticker, its own pool, its own tally, its own words for what happened
	// — and folding it in here is not about fetching better but about there being one answer to
	// how background work is retried, logged, and picked up after a restart.
	const (
		// How many pictures to line up when the queue runs dry. Enough that the runner is not
		// asking the database for more every few seconds, small enough that a first run
		// against a full database does not write thousands of rows before doing any of them.
		imageBatch = 200

		// A ceiling on the feeds one pass starts, not a limit on how many may exist:
		// whatever is left is still due and is taken next time round.
		feedBatch = 100

		// How many fetches run at once. Most of the time in a fetch is spent waiting on
		// somebody else's server, so this is about not appearing as a flood.
		feedWorkers = 6

		// How often to look for feeds that have come due. Due-ness itself is per feed and
		// lives in the feeds table — see feeds.Cadence — so this only decides the
		// granularity, which is what lets a newly added feed be fetched within the minute
		// while a weekly comic waits days.
		feedLook = time.Minute
	)

	runner := jobs.New(st, log)

	runner.Handle(feeds.MeasureImage, jobs.Work{
		Handle: feeds.Measure(st, fetcher.UserAgent()),
		// Pictures nothing has measured yet. Asked for by the runner rather than announced by
		// whoever created the work: hooks were the first design — after a fetch, after the
		// sweep, at startup — and they still missed the commonest case, because adding a feed
		// through the interface saves its articles without going near any of them.
		Refill: func(ctx context.Context) (int, error) {
			return feeds.QueueImageMeasurements(ctx, st, runner, imageBatch)
		},
	})

	runner.Handle(feeds.FetchFeed, jobs.Work{
		Handle: feeds.Fetch(st, fetcher, log),
		Refill: func(ctx context.Context) (int, error) {
			return feeds.QueueDueFeeds(ctx, st, runner, feedBatch)
		},
		Policy: jobs.Policy{
			Every:       feedLook,
			RefillEvery: feedLook,
			Batch:       feedBatch,
			Concurrency: feedWorkers,
			// One try, because a feed already has a schedule of its own. A fetch that fails
			// backs the feed off in the feeds table and comes round again as ordinary due
			// work; retrying the job as well would be two clocks disagreeing about one feed.
			MaxAttempts: 1,
		},
	})

	server := api.New(cfg, st, sessions, generator, fetcher, spa, log)

	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go sessions.Run(ctx)
	go scheduler.Run(ctx)
	go runner.Run(ctx)

	httpServer := &http.Server{
		Addr:    app.ListenAddr,
		Handler: server.Handler(),
		// A slow client must not be able to hold a connection open indefinitely.
		//
		// One response does stream — the account export, which is written as it is read
		// and can run to megabytes for a reader with years of history. It pushes its own
		// deadline forward as each batch goes out, so this ceiling applies to every
		// ordinary response and a stalled export is still cut off. See
		// api.ExportWriteWindow.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// A second server, on a port of its own, serving GET /backup and nothing else. See
	// app.BackupListenAddr for why it is a listener rather than a route.
	backupServer := &http.Server{
		Addr:    app.BackupListenAddr,
		Handler: server.BackupHandler(),
		// Longer than the reader's, alone among these. The archive is built before a byte is
		// written — a tar entry needs its size up front — so a large instance spends that
		// time inside the handler, and a backup cut off at sixty seconds would fail exactly
		// on the instances that most need one.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	errs := make(chan error, 2)
	go func() {
		log.Info("listening", "addr", app.ListenAddr, "public_url", cfg.PublicURL.String(), "version", app.Version)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	go func() {
		log.Info("serving backups; this route is unauthenticated, so do not publish the port",
			"addr", app.BackupListenAddr, "derived", cfg.BackupDerived)
		if err := backupServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdown, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	// Before the reader, and its error is dropped: a backup in flight is worth the few
	// seconds, and nothing about failing to stop it cleanly changes what the operator should
	// do next.
	_ = backupServer.Shutdown(shutdown)
	return httpServer.Shutdown(shutdown)
}

// bootstrap mints the first invitation, once, on an empty database.
//
// An invitation rather than an account with a password: no default credential exists at
// any point, so there is nothing for somebody to forget to change. The link is printed to
// the log, which is where `docker logs` will find it, and `bystander invite` reprints a
// fresh one when that has scrolled away.
func bootstrap(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) error {
	principals, err := st.ListPrincipals(ctx)
	if err != nil {
		return err
	}
	if len(principals) > 0 {
		return nil
	}

	invites, err := st.ListInvites(ctx)
	if err != nil {
		return err
	}
	// Only on a genuinely empty database. Restarting a container that nobody has signed
	// into yet must not mint a second link and leave two live.
	for _, inv := range invites {
		if inv.Usable(st.Now()) {
			log.Info("waiting for the first administrator to accept their invitation",
				"expires_at", inv.ExpiresAt.Format(time.RFC3339))
			return nil
		}
	}

	inv, token, err := st.CreateInvite(ctx, store.RoleAdmin, "", "")
	if err != nil {
		return fmt.Errorf("create the first invitation: %w", err)
	}
	log.Info("this database is empty, so here is an invitation to become its administrator",
		"url", cfg.Link("/invite/"+token),
		"expires_at", inv.ExpiresAt.Format(time.RFC3339))
	return nil
}
