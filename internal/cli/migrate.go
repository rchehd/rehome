package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/migrate"
	"github.com/rchehd/rehome/internal/source"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Upgrade the config to this rehome's format (--dry-run lists the changes)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrate(cmd)
		},
	}
}

// runMigrate pulls first, so a config another machine already migrated is only reused.
func runMigrate(cmd *cobra.Command) error {
	ctx := cmd.Context()
	root, err := config.ResolveRoot(flags.config)

	if err != nil {
		return err
	}

	s, err := config.LoadSettings()

	if err != nil {
		return err
	}

	src := source.Source(&source.Folder{Path: root})
	linked := flags.config == "" && s.Linked()

	if linked {
		src = source.Open(s, flags.dryRun, cmd.ErrOrStderr())

		if err := src.Pull(ctx); err != nil {
			return err
		}
	}

	v, err := migrate.Version(root)

	if err != nil {
		return err
	}

	if v >= config.CurrentVersion {
		cmd.Println("the config is already up to date; rehome scan adds this machine as a node")

		return nil
	}

	self, err := app.SelfNode(root)

	if err != nil {
		return err
	}

	if !flags.dryRun {
		where, err := src.Backup(ctx)

		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}

		cmd.Printf("backup: %s\n", where)
	}

	actions, err := migrate.Run(root, self, flags.dryRun)

	if err != nil {
		return err
	}

	verb := "done"

	if flags.dryRun {
		verb = "would"
	}

	for _, a := range actions {
		cmd.Printf("  %s: %s\n", verb, a)
	}

	if flags.dryRun {
		return nil
	}

	if linked {
		if err := publish(cmd, s, fmt.Sprintf("Migrate config to v%d", config.CurrentVersion)); err != nil {
			if g, ok := src.(*source.Git); ok && g.MigratedUpstream(ctx, config.CurrentVersion) {
				return adoptMigration(cmd, s, g)
			}

			return fmt.Errorf("migrated here, but publishing failed: %w; fix that and run rehome push", err)
		}
	}

	cmd.Printf("migrated to v%d as node %s; run rehome scan to refresh it\n", config.CurrentVersion, self.Name)

	return nil
}

// adoptMigration: another machine published its migration first. This one is kept on a branch,
// the clone takes the remote's config, and a scan adds this machine as a node.
func adoptMigration(cmd *cobra.Command, s config.Settings, g *source.Git) error {
	keep := "rehome-migration-" + time.Now().Format("20060102-150405")

	if err := g.SetAside(cmd.Context(), keep); err != nil {
		return fmt.Errorf("another machine migrated the config first, and taking its version failed: %w", err)
	}

	cmd.Printf("another machine migrated the config first; this machine's migration is kept on branch %s\n", keep)
	scanned, err := syncScan(cmd, false)

	if err != nil {
		return err
	}

	if !scanned.Written {
		cmd.Println("run rehome scan, then rehome push, to add this machine as a node")

		return nil
	}

	return publishChecked(cmd, s, "")
}
