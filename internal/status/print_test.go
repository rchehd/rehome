package status

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/discover"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/lock"
)

func TestLargeStatusSummaryAndCompleteDetails(t *testing.T) {
	s := &Status{Source: gitsync.SourceCurrent, MissingReasons: map[string]string{"missing-000": "script install expects ~/.oh-my-zsh/custom/plugins/example"}, Versions: map[string]AppVersions{}}
	for i := range 120 {
		s.Missing = append(s.Missing, fmt.Sprintf("missing-%03d", i))
	}
	for i := range 238 {
		s.New = append(s.New, fmt.Sprintf("new-%03d", i))
	}
	for i := range 696 {
		s.ChangedFiles = append(s.ChangedFiles, fmt.Sprintf("files/app-%02d/settings-%03d.json", i%12, i))
	}
	for i := range 61 {
		id := fmt.Sprintf("version-%03d", i)
		s.NotSynced = append(s.NotSynced, id)
		s.Versions[id] = AppVersions{Installed: lock.Entry{Manager: "apt", Version: "2.0"}, Recorded: lock.Entry{Manager: "apt", Version: "1.0"}}
	}
	var summary, details bytes.Buffer
	s.Print(&summary)
	s.PrintDetails(&details)
	if !strings.Contains(details.String(), s.MissingReasons["missing-000"]) {
		t.Fatal("details omitted the missing app's expected install path")
	}
	for _, want := range []string{"Missing apps", "120", "238", "696 files", "61", "ON MACHINE", "IN CONFIG", "2.0", "1.0", "--interactive", "--details", "rehome push", "rehome sync"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary missing %q: %s", want, &summary)
		}
	}
	if lines := strings.Count(summary.String(), "\n"); lines > 30 {
		t.Fatalf("summary still has %d lines", lines)
	}
	if strings.Contains(summary.String(), "settings-695.json") || strings.Contains(summary.String(), "missing-119") || strings.Contains(summary.String(), "new-237") {
		t.Fatal("summary printed individual paths")
	}
	for _, items := range [][]string{s.Missing, s.New, s.ChangedFiles, s.NotSynced} {
		for _, item := range items {
			if !strings.Contains(details.String(), item) {
				t.Errorf("details missing %s", item)
			}
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Status
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Missing) != 120 || len(decoded.New) != 238 || len(decoded.ChangedFiles) != 696 || len(decoded.NotSynced) != 61 {
		t.Fatalf("JSON lost data: %s", data)
	}
}

func TestStatusCleanAndInformationalWarnings(t *testing.T) {
	s := &Status{Source: gitsync.SourceCurrent, Unsupported: []string{"mac-app"}, Attention: []discover.Warning{{Path: "~/.config/Claude/claude-code-sessions", Reason: "unsupported", Action: "keep on this device"}}}
	var out bytes.Buffer
	s.Print(&out)
	if !s.Clean() || !strings.Contains(out.String(), "in sync") || !strings.Contains(out.String(), "Needs attention") {
		t.Fatalf("informational warnings changed clean status: %s", &out)
	}
	for _, source := range []gitsync.SourceStatus{gitsync.SourceAhead, gitsync.SourceChanged, gitsync.SourceUnknown, gitsync.SourceDirty} {
		s.Source = source
		if s.Clean() {
			t.Errorf("source %s must not be clean", source)
		}
	}
}

func TestMissingReasonAndSessionGrouping(t *testing.T) {
	for _, tc := range []struct {
		spec config.InstallSpec
		want string
	}{
		{config.InstallSpec{Apt: "zsh"}, "expected apt package zsh"},
		{config.InstallSpec{Script: "git clone fixture", Bin: "~/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting"}, "script install expects ~/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting"},
	} {
		if got := missingReason(tc.spec); got != tc.want {
			t.Errorf("reason = %q, want %q", got, tc.want)
		}
	}
	sections := (&Status{ChangedFiles: []string{"session missing: /home/fixture/.claude/project/1", "files/zsh/.zshrc", "unusual/path"}}).Sections()
	if section := sections[2]; section.Count != 3 || len(section.Items) != 3 || section.Items[1].Name != "sessions to restore (--sessions)" {
		t.Fatalf("unexpected file groups: %+v", section)
	}
}

func TestMissingSessionsSuggestRestore(t *testing.T) {
	s := &Status{ChangedFiles: []string{"session missing: /home/fixture/.claude/project/1"}}
	var out bytes.Buffer
	s.Print(&out)
	if !strings.Contains(out.String(), "rehome sync --sessions") || strings.Contains(out.String(), "Save this machine's changes:") {
		t.Fatalf("session restore was presented as a local change: %s", &out)
	}
}
