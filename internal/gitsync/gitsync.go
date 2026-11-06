package gitsync

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Git runs the system git with the terminal attached so credential prompts work.
type Git struct {
	DryRun bool
	Stdout io.Writer
}

func (g Git) Clone(ctx context.Context, url, path string) error {
	return g.run(ctx, "", "clone", url, path)
}

// Pull rebases local commits onto the remote, keeping uncommitted changes aside meanwhile.
func (g Git) Pull(ctx context.Context, path string) error {
	return g.run(ctx, path, "pull", "--rebase", "--autostash")
}

// Commit records every change in the repo, if there is any.
func (g Git) Commit(ctx context.Context, path, message string) error {
	if err := g.run(ctx, path, "add", "-A"); err != nil {
		return err
	}

	dirty, err := g.dirty(ctx, path)

	if err != nil || !dirty {
		return err
	}

	return g.run(ctx, path, "commit", "-m", message)
}

func (g Git) PushOnly(ctx context.Context, path string) error {
	return g.run(ctx, path, "push")
}

func (g Git) Fetch(ctx context.Context, path string) error {
	return g.run(ctx, path, "fetch", "--quiet")
}

// Upstream reads file as the last fetched upstream has it.
func (g Git) Upstream(ctx context.Context, path, file string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", "-C", path, "show", "@{upstream}:"+file).Output()
}

// AbortRebase gives up a rebase a failed pull left half done; without one it does nothing.
func (g Git) AbortRebase(ctx context.Context, path string) {
	_ = exec.CommandContext(ctx, "git", "-C", path, "rebase", "--abort").Run()
}

// SetAside keeps HEAD on branch keep and moves the clone to the fetched upstream.
func (g Git) SetAside(ctx context.Context, path, keep string) error {
	if err := g.run(ctx, path, "branch", keep); err != nil {
		return err
	}

	return g.run(ctx, path, "reset", "--hard", "--quiet", "@{upstream}")
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

	if g.Stdout != nil {
		cmd.Stdout = g.Stdout
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", args[len(args)-1], err)
	}

	return nil
}
