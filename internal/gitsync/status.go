package gitsync

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

type SourceStatus string

const (
	SourceCurrent SourceStatus = "up_to_date"
	SourceAhead   SourceStatus = "local_ahead"
	SourceDirty   SourceStatus = "local_changes"
	SourceChanged SourceStatus = "remote_changed"
	SourceUnknown SourceStatus = "unavailable"
	// SourceLocal is a folder used in place, with nothing to publish.
	SourceLocal SourceStatus = "local_folder"
)

func RemoteStatus(ctx context.Context, path string) SourceStatus {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	git := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
		cmd.WaitDelay = time.Second

		return cmd
	}

	branch, err := git("symbolic-ref", "-q", "HEAD").Output()

	if err != nil {
		return SourceUnknown
	}

	info, err := git("for-each-ref", "--format=%(objectname)%09%(upstream:remotename)%09%(upstream:remoteref)", strings.TrimSpace(string(branch))).Output()
	fields := strings.Split(strings.TrimRight(string(info), "\r\n"), "\t")

	if err != nil || len(fields) != 3 || fields[1] == "" || fields[2] == "" {
		return SourceUnknown
	}

	local, remote, ref := fields[0], fields[1], fields[2]
	out, err := git("ls-remote", "--exit-code", "--refs", "--", remote, ref).Output()
	tip, foundRef, ok := strings.Cut(strings.TrimSpace(string(out)), "\t")

	if err != nil || !ok || foundRef != ref {
		return SourceUnknown
	}

	if tip == local {
		changes, err := git("status", "--porcelain").Output()

		if err != nil {
			return SourceUnknown
		}

		if len(changes) > 0 {
			return SourceDirty
		}

		return SourceCurrent
	}

	err = git("merge-base", "--is-ancestor", tip, local).Run()

	if err == nil {
		return SourceAhead
	}

	if ctx.Err() != nil {
		return SourceUnknown
	}

	return SourceChanged
}
