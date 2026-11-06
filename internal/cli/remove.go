package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
)

type removeReport struct {
	ID         string   `json:"id"`
	Uninstall  string   `json:"uninstall,omitempty"`
	Everywhere bool     `json:"everywhere"`
	Notes      []string `json:"notes"`
}

func newRemoveCmd() *cobra.Command {
	var yes, everywhere bool

	cmd := &cobra.Command{
		Use:               "remove <app>",
		Short:             "Uninstall an app from this machine and drop it from this node",
		Long:              "With --everywhere the app is also marked removed for every node: install --all skips it and sync offers to remove it.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOwnApps,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes && !flags.dryRun && (flags.json || !interactive()) {
				return errors.New("use --yes or --dry-run in non-interactive mode")
			}

			o := opts(cmd)
			plan, err := app.PrepareRemove(cmd.Context(), o, args[0], everywhere)

			if err != nil {
				return err
			}

			if !yes && !flags.dryRun && ask(cmd, removeQuestion(plan), "n") != "y" {
				cmd.Println("nothing removed")

				return nil
			}

			notes, err := plan.Run(cmd.Context(), o)

			if err != nil {
				return err
			}

			out := removeReport{ID: plan.ID, Everywhere: everywhere, Notes: notes}

			if plan.Uninstalls() {
				out.Uninstall = plan.Command()
			}

			text := "removed " + plan.ID + " from this node"

			if everywhere {
				text += " and marked it removed everywhere; rehome push shares it"
			}

			for _, n := range notes {
				text += "\nnote: " + n
			}

			return report(cmd, out, text)
		},
	}

	cmd.Flags().BoolVar(&everywhere, "everywhere", false, "also mark the app removed for every node")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")

	return cmd
}

func removeQuestion(p *app.RemovePlan) string {
	var parts []string

	if p.Uninstalls() {
		parts = append(parts, "uninstall "+p.ID+" ("+p.Command()+")")
	} else {
		parts = append(parts, "drop "+p.ID+" from this node")
	}

	if p.Everywhere {
		parts = append(parts, "mark it removed on every node")
	}

	return fmt.Sprintf("%s? [y/N]", capitalize(strings.Join(parts, " and ")))
}

func capitalize(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

// completeOwnApps offers the apps this node recorded.
func completeOwnApps(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	w, err := app.LoadWorkspace(opts(cmd))

	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var ids []string

	for id := range w.Self.Apps {
		if strings.HasPrefix(id, toComplete) {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)

	return ids, cobra.ShellCompDirectiveNoFileComp
}
