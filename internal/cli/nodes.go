package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
)

type nodeRow struct {
	Name     string    `json:"name"`
	ID       string    `json:"id"`
	System   string    `json:"system"`
	Platform string    `json:"platform"`
	Apps     int       `json:"apps"`
	LastSeen time.Time `json:"last_seen"`
	This     bool      `json:"this"`
	Stale    bool      `json:"stale"`
}

func newNodesCmd() *cobra.Command {
	var stale bool

	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List the machines that share this config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w, err := app.LoadWorkspace(opts(cmd))

			if err != nil {
				return err
			}

			var rows []nodeRow
			now := time.Now()

			for _, n := range w.Nodes {
				r := nodeRow{Name: n.Name, ID: n.ID, System: n.System, Platform: n.Platform, Apps: len(n.Apps), LastSeen: n.LastSeen, This: n.ID == w.Self.ID, Stale: n.Stale(now, w.Settings.StaleAfter())}

				if !stale || r.Stale {
					rows = append(rows, r)
				}
			}

			if flags.json {
				return report(cmd, rows, "")
			}

			if len(rows) == 0 {
				cmd.Println("no nodes yet: rehome scan records this machine")

				return nil
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(tw, "NODE\tSYSTEM\tAPPS\tLAST SEEN\t")

			for _, r := range rows {
				note := ""

				switch {
				case r.This:
					note = "this machine"
				case r.Stale:
					note = "stale"
				}

				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", r.Name, r.System, r.Apps, seen(r.LastSeen, now), note)
			}

			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&stale, "stale", false, "show only nodes that have not pushed for a long time")
	cmd.AddCommand(newNodesRenameCmd(), newNodesForgetCmd())

	return cmd
}

func newNodesRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "rename <node> <new-name>",
		Short:             "Rename a node and its folder",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeNodes,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, n, err := findNode(cmd, args[0])

			if err != nil {
				return err
			}

			name := args[1]

			if config.NodeName(name) != name {
				return fmt.Errorf("%q is not a valid node name; try %q", name, config.NodeName(name))
			}

			if other := node.Find(w.Nodes, name); other != nil && other != n {
				return fmt.Errorf("node %s already exists", name)
			}

			dir := node.DirOf(w.Root, name)

			if flags.dryRun {
				cmd.Printf("would rename %s to %s\n", n.Name, name)

				return nil
			}

			if err := os.Rename(n.Path, dir); err != nil {
				return err
			}

			if err := config.RenameIgnore(config.FilePath(w.Root), n.Name, name); err != nil {
				return err
			}

			n.Name, n.Path = name, dir

			if err := n.Save(); err != nil {
				return err
			}

			if n.ID == w.Self.ID {
				w.Settings.NodeName = name

				if err := w.Settings.Save(); err != nil {
					return err
				}
			}

			cmd.Printf("renamed to %s; rehome push shares it\n", name)

			return nil
		},
	}
}

func newNodesForgetCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:               "forget <node>",
		Short:             "Remove a node that no longer exists from the config",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodes,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, n, err := findNode(cmd, args[0])

			if err != nil {
				return err
			}

			if n.ID == w.Self.ID {
				return errors.New("this is the current machine; it would come back on the next scan")
			}

			if flags.dryRun {
				cmd.Printf("would remove %s\n", n.Path)

				return nil
			}

			if !yes {
				if !interactive() {
					return errors.New("use --yes in non-interactive mode")
				}

				if ask(cmd, fmt.Sprintf("Remove node %s with its %d apps and settings? [y/N]", n.Name, len(n.Apps)), "n") != "y" {
					cmd.Println("nothing removed")

					return nil
				}
			}

			if err := os.RemoveAll(n.Path); err != nil {
				return err
			}

			cmd.Printf("removed %s; rehome push shares it\n", n.Name)

			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")

	return cmd
}

func findNode(cmd *cobra.Command, ref string) (*app.Workspace, *node.Node, error) {
	w, err := app.LoadWorkspace(opts(cmd))

	if err != nil {
		return nil, nil, err
	}

	n := node.Find(w.Nodes, ref)

	if n == nil {
		return nil, nil, fmt.Errorf("no node %s; rehome nodes lists them", ref)
	}

	return w, n, nil
}

func completeNodes(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	w, err := app.LoadWorkspace(opts(cmd))

	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string

	for _, n := range w.Nodes {
		names = append(names, n.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

func seen(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}

	d := now.Sub(t)

	switch {
	case d < time.Hour:
		return "just now"
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}

	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
