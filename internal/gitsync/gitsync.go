package gitsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Git runs the system git with the terminal attached so credential prompts work.
type Git struct {
	DryRun bool
}

func (g Git) Clone(ctx context.Context, url, path string) error {
	return g.run(ctx, "", "clone", url, path)
}

// Pull fast-forwards only; a diverged history is left for the user to resolve.
func (g Git) Pull(ctx context.Context, path string) error {
	return g.run(ctx, path, "pull", "--ff-only")
}

// Push commits every change in the repo (if any) and pushes it.
func (g Git) Push(ctx context.Context, path, message string) error {
	if err := g.run(ctx, path, "add", "-A"); err != nil {
		return err
	}

	dirty, err := g.dirty(ctx, path)

	if err != nil {
		return err
	}

	if dirty {
		if err := g.run(ctx, path, "commit", "-m", message); err != nil {
			return err
		}
	}

	return g.run(ctx, path, "push")
}

func (g Git) RemoteURL(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "remote", "get-url", "origin").Output()

	return strings.TrimSpace(string(out)), err
}

func (g Git) dirty(ctx context.Context, path string) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "status", "--porcelain").Output()

	return len(strings.TrimSpace(string(out))) > 0, err
}

func (g Git) run(ctx context.Context, dir string, args ...string) error {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}

	if g.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] git %s\n", strings.Join(args, " "))

		return nil
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", args[len(args)-1], err)
	}

	return nil
}
