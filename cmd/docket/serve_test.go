package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type runnerFunc func(context.Context) error

func (f runnerFunc) Run(ctx context.Context) error { return f(ctx) }

func untilDone(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func TestServeReportsEngineFailure(t *testing.T) {
	boom := errors.New("token rejected")
	err := serve(context.Background(), runnerFunc(func(context.Context) error { return boom }), untilDone)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the engine's", err)
	}
}

func TestServeStopsEngineWhenViewEnds(t *testing.T) {
	stopped := make(chan struct{})
	engine := runnerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	})
	if err := serve(context.Background(), engine, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("engine kept running after the view ended")
	}
}

func TestServeTreatsInterruptAsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serve(ctx, runnerFunc(untilDone), untilDone); err != nil {
		t.Fatalf("err = %v", err)
	}
}
