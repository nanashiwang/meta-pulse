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
}

type Observer func(name string, err error)

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
			ticker := time.NewTicker(task.Interval)
			defer ticker.Stop()
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
				if observe != nil && !(ctx.Err() != nil && errors.Is(err, context.Canceled)) {
					observe(task.Name, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(task)
	}
	workers.Wait()
	return nil
}
