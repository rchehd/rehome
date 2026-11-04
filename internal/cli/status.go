package cli

import (
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/status"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check the config source and compare the local config with this system",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			} else {
				st.Print(cmd.OutOrStdout())
			}

			if !st.Clean() {
				return ExitError{Code: 1}
			}

			return nil
		},
	}
}

func computeStatus(cmd *cobra.Command) (*status.Status, error) {
	return app.Status(cmd.Context(), opts(cmd))
}
