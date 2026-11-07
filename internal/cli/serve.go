package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/lan"
	"github.com/rchehd/rehome/internal/ui"
)

func newServeCmd() *cobra.Command {
	var pair bool
	var duration time.Duration

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Share this machine's config with paired machines on the local network until Ctrl+C",
		Long: "Share this machine's signed node and recipes with machines paired with it, read-only, over mutual TLS.\n" +
			"Nothing runs in the background: sharing stops with Ctrl+C or after --for. --pair also accepts one new pairing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := lanSettings()

			if err != nil {
				return err
			}

			id, err := lanIdentity()

			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			if duration > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, duration)
				defer cancel()
			}

			var window *lan.Window
			out := ui.New(cmd.OutOrStdout())

			if pair {
				window = lan.OpenWindow(time.Now())
				out.Section("Pairing code", ui.Bold(window.Code))
				out.List(ui.Item{Mark: ui.Plain, Text: "on the other machine run: rehome link lan, and type this code", Hint: "valid 5 minutes, 3 tries"})
				out.Println()
			}

			if _, err := share(ctx, s, id, window, cmd.ErrOrStderr()); err != nil {
				return err
			}

			out.Done(fmt.Sprintf("sharing as %s on port %d (key %s); Ctrl+C stops", s.NodeName, s.LANPort(), lan.Fingerprint(id.Public())))

			if hint := firewallHint(s.LANPort()); hint != "" {
				out.List(ui.Item{Mark: ui.Attention, Text: "a firewall is active", Hint: "if peers cannot connect: " + hint})
			}

			<-ctx.Done()

			return nil
		},
	}
	cmd.Flags().BoolVar(&pair, "pair", false, "also accept one pairing with the code shown")
	cmd.Flags().DurationVar(&duration, "for", 0, "stop sharing after this long, e.g. 10m")

	return cmd
}
