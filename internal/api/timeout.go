package api

import (
	"context"
	"time"
)

type requestTimeoutKey struct{}

// WithRequestTimeout declares a limit for one physical HTTP attempt. It does
// not start while the request waits for target-wide provider pacing. Parent
// cancellation still interrupts that wait immediately.
func WithRequestTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if timeout <= 0 {
		return ctx, cancel
	}
	return context.WithValue(ctx, requestTimeoutKey{}, timeout), cancel
}

func attemptContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout, _ := parent.Value(requestTimeoutKey{}).(time.Duration)
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}
