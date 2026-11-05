package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/selfupdate"
)

func TestUpdateCommandModes(t *testing.T) {
	previous, previousVersion := flags, Version
	t.Cleanup(func() { flags, Version = previous, previousVersion })
	Version = "0.1.0"
	for _, mode := range []string{"update", "check", "dry-run", "json", "current", "failure"} {
		t.Run(mode, func(t *testing.T) {
			flags = globalFlags{json: mode == "json", dryRun: mode == "dry-run"}
			called := false
			failure := errors.New("download failed")
			cmd := updateCommand(func(ctx context.Context, current string, options selfupdate.Options) (selfupdate.Result, error) {
				called = true
				if current != "0.1.0" || options.CheckOnly != (mode == "check") || options.DryRun != (mode == "dry-run") || ctx == nil {
					t.Fatalf("wrong update options: %s %+v", current, options)
				}
				if mode == "failure" {
					return selfupdate.Result{}, failure
				}
				return selfupdate.Result{Current: current, Latest: "0.1.1", Available: mode != "current", Updated: mode == "update" || mode == "json", CheckOnly: options.CheckOnly, DryRun: options.DryRun, Path: "/fixture/rehome"}, nil
			})
			var output, stderr bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&stderr)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			args := []string{}
			if mode == "check" {
				args = append(args, "--check")
			}
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(t.Context())
			if mode == "failure" {
				if !errors.Is(err, failure) {
					t.Fatalf("update error was swallowed: %v", err)
				}
				return
			}
			if err != nil || !called {
				t.Fatalf("command did not run: %v", err)
			}
			if mode == "json" {
				var result selfupdate.Result
				if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Updated || stderr.Len() != 0 {
					t.Fatalf("JSON output is invalid: %s / %s / %v", &output, &stderr, err)
				}
			} else if !strings.Contains(output.String(), "0.1.0") || !strings.Contains(output.String(), "0.1.1") {
				t.Fatalf("versions missing from output: %s", &output)
			}
		})
	}
}

func TestUpdateCommandIsRegistered(t *testing.T) {
	previous := flags
	t.Cleanup(func() { flags = previous })
	cmd, _, err := newRoot().Find([]string{"update"})
	if err != nil || cmd.Name() != "update" || cmd.Flags().Lookup("check") == nil {
		t.Fatalf("update command is missing: %v", err)
	}
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Fatal("unexpected arguments accepted")
	}
}
