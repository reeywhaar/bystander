package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	"bystander/web"
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

	spa, err := api.NewSPA(web.Dist(), log)
	if err != nil {
		return fmt.Errorf("load the frontend: %w", err)
	}

	sessions := session.New(st, cfg.Secure, log)
	fetcher := feeds.NewFetcher(cfg.PublicURL.String())
	poller := feeds.NewPoller(st, fetcher, cfg.FetchInterval, log)
	generator := edition.NewGenerator(st, log)
	scheduler := edition.NewScheduler(st, generator, log)

	// Background work that is nobody's request: measuring the pictures a feed brought in, and
	// whatever else grows to need the same treatment. Every handler is registered here so
	// that one file lists everything this program does when nobody is looking at it.
	// How many measurements to line up when the queue runs dry. Enough that the runner is not
	// asking the database for more every few seconds, small enough that a first run against a
	// full database does not write thousands of rows before doing any of them.
	const queueBatch = 200

	runner := jobs.New(st, log)
	runner.Handle(feeds.MeasureImage, feeds.Measure(st, fetcher.UserAgent()))

	// Two ways work reaches the queue, and they answer two different questions.
	//
	// A fetch that brought articles in queues their pictures: that is the moment work
	// appears, and asking then rather than on a timer keeps an idle instance idle.
	//
	// The sweep is the safety net, once an hour. It catches everything the first path cannot
	// know about — articles that were already here when this feature shipped, a queue lost to
	// a restore, a hook that failed. Enqueueing is idempotent on the URL, so the two paths
	// asking for the same picture is one row.
	queuePictures := func(ctx context.Context) {
		n, err := feeds.QueueImageMeasurements(ctx, st, runner, queueBatch)
		if err != nil {
			log.Error("could not queue picture measurements", "error", err)
		} else if n > 0 {
			log.Debug("queued picture measurements", "count", n)
		}
	}
	poller.AfterNewArticles(queuePictures)
	scheduler.AfterSweep(queuePictures)

	server := api.New(cfg, st, sessions, generator, fetcher, spa, log)

	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go sessions.Run(ctx)
	go poller.Run(ctx)
	go scheduler.Run(ctx)
	// The queue is topped up at the start of each pass rather than by whoever created the
	// work. A picture arrives through a poll, through somebody adding a feed, or through a
	// database restored from a backup, and asking each of those to remember to enqueue is
	// three places for the answer to be no.
	go runner.Run(ctx)

	httpServer := &http.Server{
		Addr:    app.ListenAddr,
		Handler: server.Handler(),
		// A slow client must not be able to hold a connection open indefinitely. The
		// write timeout is generous because nothing here streams.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", app.ListenAddr, "public_url", cfg.PublicURL.String(), "version", app.Version)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	inv, token, err := st.CreateInvite(ctx, store.RoleAdmin, "")
	if err != nil {
		return fmt.Errorf("create the first invitation: %w", err)
	}
	log.Info("this database is empty, so here is an invitation to become its administrator",
		"url", cfg.Link("/invite/"+token),
		"expires_at", inv.ExpiresAt.Format(time.RFC3339))
	return nil
}
