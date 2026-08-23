// Package cli is bystander's command line: the daemon, and the few things an operator
// needs a shell for.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"bystander/internal/app"
	"bystander/internal/config"
	"bystander/internal/store"
)

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bystander",
		Short: "An RSS reader with no unread count",
		Long: "bystander fetches feeds on a schedule and composes a front page from them.\n" +
			"When the next page is generated the previous one is gone for good.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Cobra's Print helpers write to stderr unless an output is set, which would make
	// `link=$(bystander invite)` capture nothing at all — the one thing anybody wants to
	// do with that command. Errors still go to stderr, from Execute below.
	cmd.SetOut(os.Stdout)
	cmd.AddCommand(serveCmd(), inviteCmd(), healthcheckCmd(), versionCmd())
	return cmd
}

// Execute runs the command line and returns a process exit code.
func Execute() int {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "bystander:", err)
		return 1
	}
	return 0
}

// setup is what every command that touches data needs: the environment, a logger, and both
// databases.
func setup() (*config.Config, *store.Store, *slog.Logger, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, st, log, nil
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version this binary was built from",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println(app.Version)
			return nil
		},
	}
}
