// Package progress passes the name of the current long-running step to whoever displays it.
package progress

import (
	"context"
	"fmt"
)

type key struct{}

// With returns a context whose steps are passed to report.
func With(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, key{}, report)
}

// Step reports the step that starts now; without a reporter it does nothing.
func Step(ctx context.Context, format string, args ...any) {
	if report, ok := ctx.Value(key{}).(func(string)); ok {
		report(fmt.Sprintf(format, args...))
	}
}
