package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"simple", "simple"},
		{"a b", "'a b'"},
		{"it's", `'it'\''s'`},
		{"$HOME", "'$HOME'"},
		{"", "''"},
	}

	for _, tt := range tests {
		if got := Quote(tt.in); got != tt.want {
			t.Errorf("Quote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestQuoteRoundTripsThroughBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	for _, s := range []string{"simple", "a b", "it's", "$HOME", "", "a;b|c", "`x`", `back\slash`, "tab\there"} {
		out, err := exec.Command("bash", "-c", "printf %s "+Quote(s)).Output()

		if err != nil {
			t.Fatal(err)
		}

		if string(out) != s {
			t.Errorf("%q round-tripped to %q", s, out)
		}
	}
}

func TestCmdString(t *testing.T) {
	tests := []struct {
		name string
		cmd  Cmd
		want string
	}{
		{"plain", Cmd{Name: "apt-get", Args: []string{"install", "-y", "a b"}}, "apt-get install -y 'a b'"},
		{"sudo", Cmd{Name: "mv", Args: []string{"a", "b"}, Sudo: true}, "sudo mv a b"},
		{"shell", Cmd{Shell: "curl x | sh"}, "bash -o pipefail -c 'curl x | sh'"},
		{"sudo shell", Cmd{Shell: "echo hi", Sudo: true}, "sudo bash -o pipefail -c 'echo hi'"},
	}

	for _, tt := range tests {
		if got := tt.cmd.String(); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDryRunDoesNotExecute(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	var log bytes.Buffer
	r := &Runner{DryRun: true, Log: &log}

	res, err := r.Run(context.Background(), Cmd{Name: "touch", Args: []string{marker}})

	if err != nil {
		t.Fatal(err)
	}

	if res.Stdout != "" || res.ExitCode != 0 {
		t.Errorf("result = %+v", res)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Error("command was executed in dry-run")
	}

	if !strings.Contains(log.String(), "[dry-run] touch") {
		t.Errorf("log = %q", log.String())
	}
}

func TestRunExecutes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	r := &Runner{Log: &bytes.Buffer{}}

	if _, err := r.Run(context.Background(), Cmd{Name: "touch", Args: []string{marker}}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Error("command was not executed")
	}
}

func TestRunReportsExitError(t *testing.T) {
	r := &Runner{Log: &bytes.Buffer{}}

	_, err := r.Run(context.Background(), Cmd{Shell: "echo oops >&2; exit 3"})
	ce, ok := err.(*CmdError)

	if !ok {
		t.Fatalf("err = %v", err)
	}

	if ce.ExitCode != 3 || !strings.Contains(ce.StderrTail, "oops") {
		t.Errorf("cmd error = %+v", ce)
	}
}

func TestOutputRunsInDryRun(t *testing.T) {
	r := &Runner{DryRun: true, Log: &bytes.Buffer{}}

	out, err := r.Output(context.Background(), Cmd{Name: "echo", Args: []string{"hello"}})

	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(out) != "hello" {
		t.Errorf("out = %q", out)
	}
}
