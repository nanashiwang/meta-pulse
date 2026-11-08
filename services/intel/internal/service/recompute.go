package service

import (
	"context"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
)

type SnapshotWriter interface {
	LoadInputs(ctx context.Context, now time.Time, speedWindow time.Duration) (Inputs, error)
	SaveSnapshot(ctx context.Context, snap Snapshot) (bool, error)
}

// Recompute builds the board from current data and stores it if it differs
// from every stored board. Running it repeatedly is safe: an unchanged board
// is recognised by fingerprint and not written again.
func Recompute(ctx context.Context, store SnapshotWriter, method score.Method, now time.Time, speedWindow time.Duration) (Snapshot, bool, error) {
	in, err := store.LoadInputs(ctx, now, speedWindow)
	if err != nil {
		return Snapshot{}, false, err
	}
	snap, err := BuildSnapshot(method, in, now)
	if err != nil {
		return Snapshot{}, false, err
	}
	stored, err := store.SaveSnapshot(ctx, snap)
	return snap, stored, err
}
