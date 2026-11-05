package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/progress"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner draws the current progress step on stderr; a nil spinner does nothing.
type spinner struct {
	mu      sync.Mutex
	out     io.Writer
	step    string
	start   time.Time
	frame   int
	stopped bool
	stop    chan struct{}
	done    chan struct{}
}

// startSpinner runs a spinner unless the output is JSON, verbose or not a terminal.
func startSpinner(cmd *cobra.Command) *spinner {
	if flags.json || flags.verbose || !isatty.IsTerminal(os.Stderr.Fd()) {
		return nil
	}

	s := &spinner{out: cmd.ErrOrStderr(), step: "starting", start: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}

	go s.loop()

	return s
}

// Context makes progress.Step calls on ctx update the spinner.
func (s *spinner) Context(ctx context.Context) context.Context {
	if s == nil {
		return ctx
	}

	return progress.With(ctx, func(step string) {
		s.mu.Lock()
		s.step = step
		s.draw()
		s.mu.Unlock()
	})
}

// Writer returns w wrapped so its lines print above the spinner.
func (s *spinner) Writer(w io.Writer) io.Writer {
	if s == nil {
		return w
	}

	return writerFunc(func(p []byte) (int, error) {
		s.mu.Lock()
		defer s.mu.Unlock()

		if !s.stopped {
			fmt.Fprint(s.out, "\r\033[K")
		}

		n, err := w.Write(p)
		s.draw()

		return n, err
	})
}

// Stop clears the spinner line; it is safe to call more than once.
func (s *spinner) Stop() {
	if s == nil {
		return
	}

	s.mu.Lock()

	if s.stopped {
		s.mu.Unlock()

		return
	}

	s.stopped = true
	close(s.stop)
	fmt.Fprint(s.out, "\r\033[K")
	s.mu.Unlock()
	<-s.done
}

func (s *spinner) loop() {
	defer close(s.done)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.frame++
			s.draw()
			s.mu.Unlock()
		}
	}
}

// draw must be called with mu held.
func (s *spinner) draw() {
	if s.stopped {
		return
	}

	fmt.Fprintf(s.out, "\r\033[K%s %s (%ds)", frames[s.frame%len(frames)], s.step, int(time.Since(s.start).Seconds()))
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

// withSpinner runs fn under a spinner, with the log printing above it.
func withSpinner[T any](cmd *cobra.Command, o app.Options, fn func(context.Context, app.Options) (T, error)) (T, error) {
	sp := startSpinner(cmd)
	defer sp.Stop()

	o.Log = sp.Writer(o.Log)

	return fn(sp.Context(cmd.Context()), o)
}
