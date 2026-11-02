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
	{Key: "status", Title: "Status", Help: "compare the config with this system"},
	{Key: "pull", Title: "Pull", Help: "fetch the config from your repo"},
	{Key: "push", Title: "Push", Help: "commit and push the config to your repo"},
}

var menuCommands = map[string]func() *cobra.Command{
	"scan": newScanCmd, "install": newInstallCmd, "status": newStatusCmd, "pull": newPullCmd, "push": newPushCmd,
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

		if err := sub.RunE(sub, nil); err != nil {
			if _, isExit := err.(ExitError); !isExit {
				cmd.PrintErrln("error:", err)
			}
		}

		ask(cmd, "\npress enter to return to the menu", "")
	}
}
