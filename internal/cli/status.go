package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/status"
	"github.com/rchehd/rehome/internal/tui"
)

func newStatusCmd() *cobra.Command {
	var details, browse bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check the config source and compare the local config with this system",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if browse && (details || flags.json) {
				return errors.New("--interactive cannot be combined with --details or --json")
			}
			if browse && !interactive() {
				return errors.New("--interactive needs a terminal; use --details or --json when redirecting output")
			}
			st, err := computeStatus(cmd)

			if err != nil {
				return err
			}

			if flags.config == "" {
				s, err := config.LoadSettings()

				if err != nil {
					return err
				}

				if s.Linked() {
					st.Source = gitsync.RemoteStatus(cmd.Context(), s.ClonePath)
				}
			}

			if flags.json {
				if err := report(cmd, struct {
					Clean bool `json:"clean"`
					*status.Status
				}{st.Clean(), st}, ""); err != nil {
					return err
				}
			} else if browse {
				if err := tui.Status(cmd.Context(), st, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
					return err
				}
			} else if details {
				st.PrintDetails(cmd.OutOrStdout())
			} else {
				st.Print(cmd.OutOrStdout())
			}

			if !st.Clean() {
				return ExitError{Code: 1}
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&browse, "interactive", false, "Browse differences by category with search and scrolling")
	cmd.Flags().BoolVar(&details, "details", false, "Show every difference instead of a compact summary")
	return cmd
}

func computeStatus(cmd *cobra.Command) (*status.Status, error) {
	return app.Status(cmd.Context(), opts(cmd))
}
