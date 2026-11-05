package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/tui"
)

var menuItems = []tui.MenuItem{
	{Key: "scan", Title: "Scan", Help: "detect software and settings, update the config"},
	{Key: "install", Title: "Install", Help: "pick and install what is missing"},
	{Key: "sync", Title: "Sync", Help: "pull and apply the config"},
	{Key: "status", Title: "Status", Help: "check the config source and this system"},
	{Key: "pull", Title: "Pull", Help: "fetch the config from your repo"},
	{Key: "push", Title: "Push", Help: "commit and push the config to your repo"},
}

var menuCommands = map[string]func() *cobra.Command{
	"scan": newScanCmd, "install": newInstallCmd, "sync": newSyncCmd, "status": newStatusCmd, "pull": newPullCmd, "push": newPushCmd,
}

// runMenu loops over the main menu until the user quits.
func runMenu(cmd *cobra.Command) error {
	for {
		header := "no config linked"

		if s, err := config.LoadSettings(); err == nil && s.Linked() {
			header = s.RepoURL
		}

		choice, err := tui.Menu(header, menuItems)

		if err != nil || choice == "" {
			return err
		}

		sub := menuCommands[choice]()
		sub.SetContext(cmd.Context())
		sub.SetOut(os.Stdout)
		if choice == "status" {
			if err := sub.Flags().Set("interactive", "true"); err != nil {
				return err
			}
		}

		runErr := sub.RunE(sub, nil)
		if runErr != nil {
			if _, isExit := runErr.(ExitError); !isExit {
				cmd.PrintErrln("error:", runErr)
			}
		}

		_, isExit := runErr.(ExitError)
		if choice == "status" && (runErr == nil || isExit) {
			continue
		}
		ask(cmd, "\npress enter to return to the menu", "")
	}
}
