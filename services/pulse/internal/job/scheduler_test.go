package job

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlowSourceCannotStarveSettlementAndTimeoutsAreFresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slowStarted := make(chan struct{})
	completed := make(chan error, 8)
	expired := make(chan error, 1)
	done := make(chan error, 1)
	var active, maxActive atomic.Int32
	go func() {
		done <- Run(ctx, []Task{
			{Name: "ingest", Interval: time.Millisecond, Timeout: 40 * time.Millisecond, Run: func(ctx context.Context) error {
				if n := active.Add(1); n > 1 {
					maxActive.Store(n)
				}
				defer active.Add(-1)
				select {
				case <-slowStarted:
				default:
					close(slowStarted)
				}
				<-ctx.Done()
				select {
				case expired <- ctx.Err():
				default:
				}
				return ctx.Err()
			}},
			{Name: "settlement", Interval: 5 * time.Millisecond, Timeout: time.Second, Run: func(ctx context.Context) error {
				select {
				case completed <- ctx.Err():
				default:
				}
				return nil
			}},
		}, nil)
	}()
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("source never started")
	}
	// Settlement progresses while the slow source is still waiting.
	for i := 0; i < 3; i++ {
		select {
		case err := <-completed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("source starved settlement")
		}
	}
	select {
	case err := <-expired:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("source timeout missing")
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal("settlement inherited expired context", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("settlement stopped after source timeout")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not stop tasks")
	}
	if maxActive.Load() > 1 {
		t.Fatal("same task overlapped")
	}
}

func TestSchedulerRejectsInvalidTasks(t *testing.T) {
	if err := Run(context.Background(), []Task{{Name: "missing-run"}}, nil); err == nil {
		t.Fatal("invalid task accepted")
	}
}
