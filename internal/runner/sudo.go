package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const sudoRefresh = 60 * time.Second

// StartSudo asks for the sudo password once and keeps the credential cache warm until stop is called.
func StartSudo(ctx context.Context) (stop func(), err error) {
	if os.Geteuid() == 0 {
		return func() {}, nil
	}

	cmd := exec.CommandContext(ctx, "sudo", "-v")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sudo: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		t := time.NewTicker(sudoRefresh)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = exec.CommandContext(ctx, "sudo", "-n", "-v").Run()
			}
		}
	}()

	return cancel, nil
}
