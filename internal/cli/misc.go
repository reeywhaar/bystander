package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"bystander/internal/app"
	"bystander/internal/store"
)

func inviteCmd() *cobra.Command {
	var asUser bool

	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Mint an invitation link",
		Long: "Mints a link somebody can use to create an account.\n\n" +
			"This is the way back in when the first link has scrolled out of the log, and the\n" +
			"only way in that does not require an account already.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, st, _, err := setup()
			if err != nil {
				return err
			}
			defer st.Close()

			role := store.RoleAdmin
			if asUser {
				role = store.RoleUser
			}

			inv, token, err := st.CreateInvite(cmd.Context(), role, "", "")
			if err != nil {
				return err
			}
			cmd.Println(cfg.Link("/invite/" + token))
			cmd.Printf("%s, expires %s\n", role, inv.ExpiresAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asUser, "user", false, "mint an ordinary account rather than an administrator")
	return cmd
}

// healthcheckCmd is what the image's HEALTHCHECK runs.
//
// The binary calls itself over loopback rather than shelling out to wget, so the image
// needs no HTTP client. A loopback GET exercises the listener, the router and the handler
// chain, so a process that is running but wedged fails it.
func healthcheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Check that a local instance is answering",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			// The listen address is not configurable, so this needs no environment and
			// works in a container that has none set.
			target := "http://127.0.0.1" + app.ListenAddr + "/healthz"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return err
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("healthz answered %s", res.Status)
			}
			var body struct {
				OK bool `json:"ok"`
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil || !body.OK {
				return fmt.Errorf("healthz did not report itself healthy")
			}
			return nil
		},
	}
}
