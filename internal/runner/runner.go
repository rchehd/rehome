package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const stderrTailLines = 20

type Cmd struct {
	Name string
	Args []string
	// Sudo runs the command through sudo.
	Sudo bool
	// Shell, when set, is run through `bash -c` instead of Name/Args.
	Shell string
	Env   []string
}

type Result struct {
	Stdout     string
	StderrTail string
	ExitCode   int
}

type Runner struct {
	DryRun  bool
	Verbose bool
	// Log receives dry-run and verbose output; os.Stderr when nil.
	Log io.Writer
}

// Run executes a state-changing command; in dry-run mode it only prints it.
func (r *Runner) Run(ctx context.Context, c Cmd) (Result, error) {
	if r.DryRun {
		r.logf("[dry-run] %s\n", c)

		return Result{}, nil
	}

	return r.exec(ctx, c)
}

// Output executes a read-only command even in dry-run mode and returns its stdout.
func (r *Runner) Output(ctx context.Context, c Cmd) (string, error) {
	res, err := r.exec(ctx, c)

	return res.Stdout, err
}

func (r *Runner) exec(ctx context.Context, c Cmd) (Result, error) {
	if r.Verbose {
		r.logf("$ %s\n", c)
	}

	argv := c.argv()

	if path, err := LookPath(argv[0]); err == nil {
		argv[0] = path
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(append(os.Environ(), "PATH="+SearchPath()), c.Env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.String(), StderrTail: tail(stderr.String(), stderrTailLines)}

	if err == nil {
		return res, nil
	}

	var exitErr *exec.ExitError

	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()

		return res, &CmdError{Cmd: c.String(), ExitCode: res.ExitCode, StderrTail: res.StderrTail}
	}

	return res, fmt.Errorf("%s: %w", c, err)
}

func (r *Runner) logf(format string, args ...any) {
	w := r.Log

	if w == nil {
		w = os.Stderr
	}

	fmt.Fprintf(w, format, args...)
}

type CmdError struct {
	Cmd        string
	ExitCode   int
	StderrTail string
}

func (e *CmdError) Error() string {
	msg := fmt.Sprintf("%s: exit status %d", e.Cmd, e.ExitCode)

	if e.StderrTail != "" {
		msg += "\n" + e.StderrTail
	}

	return msg
}

func (c Cmd) argv() []string {
	argv := append([]string{c.Name}, c.Args...)

	// pipefail makes `curl … | sh` fail when the download does.
	if c.Shell != "" {
		argv = []string{"bash", "-o", "pipefail", "-c", c.Shell}
	}

	if c.Sudo {
		argv = append([]string{"sudo"}, argv...)
	}

	return argv
}

func (c Cmd) String() string {
	argv := c.argv()
	quoted := make([]string, len(argv))

	for i, a := range argv {
		quoted[i] = Quote(a)
	}

	return strings.Join(quoted, " ")
}

// Quote makes s a single shell word.
func Quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`|&;<>()*?[]{}!#~") {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
