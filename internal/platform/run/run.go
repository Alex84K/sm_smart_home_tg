package run

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
)

// Runner represents a long-running background component (ADR-0016).
type Runner interface {
	Run(ctx context.Context) error
}

// RunnerFunc is an adapter to allow the use of ordinary functions as Runners.
type RunnerFunc func(ctx context.Context) error

// Run calls f(ctx).
func (f RunnerFunc) Run(ctx context.Context) error {
	return f(ctx)
}

// Group coordinates multiple Runners using errgroup and cancels when ctx is canceled.
type Group struct {
	runners []Runner
}

// NewGroup creates a new Group with the provided runners.
func NewGroup(runners ...Runner) *Group {
	return &Group{runners: runners}
}

// Add adds one or more runners to the group.
func (g *Group) Add(runners ...Runner) {
	g.runners = append(g.runners, runners...)
}

// Run executes all runners in the group concurrently.
// If any runner returns an error or if ctx is canceled, all other runners are canceled.
func (g *Group) Run(ctx context.Context) error {
	gCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	eg, egCtx := errgroup.WithContext(gCtx)

	for _, r := range g.runners {
		runner := r
		eg.Go(func() error {
			return runner.Run(egCtx)
		})
	}

	return eg.Wait()
}

// SignalContext returns a context that is canceled when SIGINT or SIGTERM is received.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
