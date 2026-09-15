// Package job isolates background tasks from one another. Scheduling is not a
// financial lock: durable transactions and outbox fences remain authoritative.
package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Task struct {
	Name     string
	Interval time.Duration
	Timeout  time.Duration
	Run      func(context.Context) error

	// MaxBackoff caps the delay applied after consecutive failures. Zero
	// disables backoff and the task retries every Interval regardless of
	// outcome. Set it on tasks whose failure mode is an expensive dependency
	// call: retrying a query that times out at fixed Interval turns one slow
	// statement into sustained load on the dependency, which is how a stuck
	// cursor keeps a remote database saturated indefinitely.
	MaxBackoff time.Duration
}

type Observer func(name string, err error)

// nextDelay returns how long to wait before a task's next attempt. Failures
// double the delay from Interval up to MaxBackoff; any success resets it.
func nextDelay(task Task, consecutiveFailures int) time.Duration {
	if task.MaxBackoff <= 0 || consecutiveFailures <= 0 {
		return task.Interval
	}
	delay := task.Interval
	for i := 0; i < consecutiveFailures; i++ {
		if delay >= task.MaxBackoff/2 {
			return task.MaxBackoff
		}
		delay *= 2
	}
	if delay > task.MaxBackoff {
		return task.MaxBackoff
	}
	return delay
}

// Run starts one non-overlapping loop per task and waits for cancellation.
// Each invocation derives its timeout from the root context, never another job.
func Run(ctx context.Context, tasks []Task, observe Observer) error {
	seen := make(map[string]bool)
	for _, task := range tasks {
		if task.Name == "" || seen[task.Name] || task.Interval <= 0 || task.Timeout <= 0 || task.Run == nil {
			return fmt.Errorf("invalid or duplicate task %q", task.Name)
		}
		seen[task.Name] = true
	}
	var workers sync.WaitGroup
	for _, task := range tasks {
		workers.Add(1)
		go func(task Task) {
			defer workers.Done()
			timer := time.NewTimer(task.Interval)
			defer timer.Stop()
			consecutiveFailures := 0
			for {
				if ctx.Err() != nil {
					return
				}
				taskCtx, cancel := context.WithTimeout(ctx, task.Timeout)
				err := task.Run(taskCtx)
				if err == nil {
					err = taskCtx.Err()
				}
				cancel()
				// Normal process shutdown is not an operational job failure.
				shuttingDown := ctx.Err() != nil && errors.Is(err, context.Canceled)
				if observe != nil && !shuttingDown {
					observe(task.Name, err)
				}
				if err != nil && !shuttingDown {
					consecutiveFailures++
				} else {
					consecutiveFailures = 0
				}
				timer.Reset(nextDelay(task, consecutiveFailures))
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
		}(task)
	}
	workers.Wait()
	return nil
}
