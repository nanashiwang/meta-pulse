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

func TestNextDelayBacksOffOnConsecutiveFailuresAndResetsOnSuccess(t *testing.T) {
	task := Task{Interval: 30 * time.Second, MaxBackoff: 10 * time.Minute}
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: 30 * time.Second},
		{failures: 1, want: time.Minute},
		{failures: 2, want: 2 * time.Minute},
		{failures: 3, want: 4 * time.Minute},
		{failures: 4, want: 8 * time.Minute},
		// Capped, and stays capped however long the dependency stays down.
		{failures: 5, want: 10 * time.Minute},
		{failures: 50, want: 10 * time.Minute},
	}
	for _, tt := range tests {
		if got := nextDelay(task, tt.failures); got != tt.want {
			t.Fatalf("nextDelay(failures=%d) = %v, want %v", tt.failures, got, tt.want)
		}
	}
}

func TestNextDelayWithoutMaxBackoffKeepsFixedInterval(t *testing.T) {
	task := Task{Interval: 30 * time.Second}
	for _, failures := range []int{0, 1, 9} {
		if got := nextDelay(task, failures); got != task.Interval {
			t.Fatalf("nextDelay(failures=%d) = %v, want %v", failures, got, task.Interval)
		}
	}
}

// A task whose dependency is timing out must not keep calling it at the base
// interval: that is what turned one slow LOG_DB query into sustained load.
func TestFailingTaskIsRetriedLessOftenThanInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []Task{
			{Name: "flaky", Interval: 5 * time.Millisecond, Timeout: time.Second, MaxBackoff: time.Second,
				Run: func(context.Context) error {
					attempts.Add(1)
					return errors.New("dependency timed out")
				}},
		}, nil)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	// Without backoff this window would allow roughly 24 attempts; the
	// doubling delay must keep it far below that.
	if got := attempts.Load(); got > 8 {
		t.Fatalf("failing task ran %d times, backoff did not apply", got)
	}
	if attempts.Load() == 0 {
		t.Fatal("task never ran")
	}
}

// Backoff must not make a healthy task slower.
func TestSucceedingTaskKeepsBaseInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []Task{
			{Name: "healthy", Interval: 5 * time.Millisecond, Timeout: time.Second, MaxBackoff: time.Second,
				Run: func(context.Context) error {
					attempts.Add(1)
					return nil
				}},
		}, nil)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done
	if got := attempts.Load(); got < 8 {
		t.Fatalf("healthy task ran only %d times, backoff leaked into the success path", got)
	}
}
