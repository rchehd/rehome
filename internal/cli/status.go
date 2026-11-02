package cli

import (
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/status"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Compare the config with this system; exits 1 when they differ",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := computeStatus(cmd)

			if err != nil {
				return err
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
