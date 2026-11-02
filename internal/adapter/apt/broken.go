package apt

import (
	"context"
	"fmt"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/runner"
)

// Broken lists packages dpkg left unpacked, half-installed or half-configured. One such
// package makes every later apt-get install fail while dpkg retries its configuration.
func Broken(ctx context.Context, r *runner.Runner) ([]string, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Abbrev}|${Package}\n"}})

	if err != nil {
		return nil, err
	}

	var broken []string

	for _, l := range adapter.Lines(out) {
		abbrev, pkg, ok := strings.Cut(l, "|")

		if ok && len(abbrev) >= 2 && strings.ContainsRune("UFH", rune(abbrev[1])) {
			broken = append(broken, pkg)
		}
	}

	return broken, nil
}

// RemoveBroken drops packages whose installation failed so apt works again. When their
// removal scripts fail too (e.g. they call systemctl without systemd), those scripts are
// replaced with no-ops first; callers pass only packages broken during this run.
func RemoveBroken(ctx context.Context, r *runner.Runner, pkgs []string) error {
	args := append([]string{"--remove", "--force-remove-reinstreq", "--force-depends"}, pkgs...)
	remove := runner.Cmd{Name: "dpkg", Args: args, Sudo: true}

	if _, err := r.Run(ctx, remove); err == nil {
		return nil
	}

	var script strings.Builder

	for _, p := range pkgs {
		for _, kind := range []string{"prerm", "postrm", "postinst"} {
			f := runner.Quote("/var/lib/dpkg/info/" + p + "." + kind)
			fmt.Fprintf(&script, "[ -f %s ] && printf '#!/bin/sh\\nexit 0\\n' > %s; ", f, f)
		}
	}

	if _, err := r.Run(ctx, runner.Cmd{Shell: script.String() + "true", Sudo: true}); err != nil {
		return err
	}

	_, err := r.Run(ctx, remove)

	return err
}
