// Package health contains dependency checks used by liveness and readiness
// endpoints. Liveness never probes dependencies; readiness does.
package health

import (
	"context"
	"errors"
	"fmt"
)

// Pinger is implemented by dependencies that can verify their connectivity.
type Pinger interface {
	Ping(context.Context) error
}

// Checker checks every configured runtime dependency.
type Checker struct {
	dependencies map[string]Pinger
}

func NewChecker(dependencies map[string]Pinger) *Checker {
	copy := make(map[string]Pinger, len(dependencies))
	for name, dependency := range dependencies {
		if dependency != nil {
			copy[name] = dependency
		}
	}
	return &Checker{dependencies: copy}
}

// Check returns a combined error. A missing dependency is a configuration
// error, not a successful readiness result.
func (c *Checker) Check(ctx context.Context) error {
	if c == nil || len(c.dependencies) == 0 {
		return errors.New("no runtime dependencies configured")
	}

	var errs []error
	for name, dependency := range c.dependencies {
		if err := dependency.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
