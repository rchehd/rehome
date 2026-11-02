package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
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

			st.Print(cmd.OutOrStdout())

			if !st.Clean() {
				return ExitError{Code: 1}
			}

			return nil
		},
	}
}

func computeStatus(cmd *cobra.Command) (*status.Status, error) {
	cfg, root, err := loadConfig(cmd)

	if err != nil {
		return nil, err
	}

	existing, err := os.ReadFile(filepath.Join(root, config.FileName))

	if err != nil {
		return nil, err
	}

	p, err := platform.Detect()

	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()

	if err != nil {
		return nil, err
	}

	repo, err := lock.Load(lock.Path(root, flags.lock))

	if err != nil {
		return nil, err
	}

	localPath, err := lock.LocalPath()

	if err != nil {
		return nil, err
	}

	local, err := lock.Load(localPath)

	if err != nil {
		return nil, err
	}

	return status.Compute(cmd.Context(), newRunner(), cfg, existing, root, p, cat, repo, local)
}
