package run_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Alex84K/tg_gateway_go/internal/platform/run"
)

func TestGroupRunCancellation(t *testing.T) {
	var count int32

	r1 := run.RunnerFunc(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		<-ctx.Done()
		return nil
	})

	r2 := run.RunnerFunc(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		<-ctx.Done()
		return nil
	})

	group := run.NewGroup(r1)
	group.Add(r2)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- group.Run(ctx)
	}()

	// Wait briefly for runners to start
	for atomic.LoadInt32(&count) < 2 {
		time.Sleep(10 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error on normal cancellation, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for group to stop")
	}
}

func TestGroupRunErrorPropagation(t *testing.T) {
	expectedErr := errors.New("worker failed")

	failingRunner := run.RunnerFunc(func(ctx context.Context) error {
		return expectedErr
	})

	waitingRunner := run.RunnerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	group := run.NewGroup(failingRunner, waitingRunner)
	err := group.Run(context.Background())

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}
