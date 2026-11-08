// Package admin implements the operations behind the signed admin API. The
// forum plugin authenticates the Answer administrator; Intel trusts only the
// signed user ID it forwards and records it on every import.
package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

type Store interface {
	service.SnapshotWriter
	Known(ctx context.Context) (catalog.Known, error)
	ImportAs(ctx context.Context, b catalog.Batch, meta mysqlstore.ImportMeta) (mysqlstore.ImportStats, error)
	Overview(ctx context.Context, method string) (mysqlstore.Overview, error)
	Export(ctx context.Context, now time.Time) (catalog.Document, error)
}

type Service struct {
	Store       Store
	Method      score.Method
	SpeedWindow time.Duration
	Now         func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Overview(ctx context.Context) (mysqlstore.Overview, error) {
	return s.Store.Overview(ctx, s.Method.Version)
}

func (s *Service) Export(ctx context.Context) (catalog.Document, error) {
	return s.Store.Export(ctx, s.now())
}

// Validate checks a document against the stored catalog without writing.
func (s *Service) Validate(ctx context.Context, doc catalog.Document) (map[string]int, error) {
	known, err := s.Store.Known(ctx)
	if err != nil {
		return nil, err
	}
	b, err := catalog.Validate(doc, known)
	if err != nil {
		return nil, err
	}
	return b.Summary(), nil
}

type ImportResult struct {
	Stats    mysqlstore.ImportStats `json:"stats"`
	Snapshot SnapshotResult         `json:"snapshot"`
}

type SnapshotResult struct {
	Stored        bool      `json:"stored"`
	Rows          int       `json:"rows"`
	MethodVersion string    `json:"method_version"`
	ComputedAt    time.Time `json:"computed_at"`
}

// Import validates, writes and rescores. The request fingerprint is the hash
// of the exact body, so reusing a key with different content is a conflict.
func (s *Service) Import(ctx context.Context, doc catalog.Document, raw []byte, actorID, key string) (ImportResult, error) {
	known, err := s.Store.Known(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	b, err := catalog.Validate(doc, known)
	if err != nil {
		return ImportResult{}, err
	}
	sum := sha256.Sum256(raw)
	st, err := s.Store.ImportAs(ctx, b, mysqlstore.ImportMeta{ActorID: actorID, IdempotencyKey: key, RequestFingerprint: hex.EncodeToString(sum[:])})
	if err != nil {
		return ImportResult{}, err
	}
	snap, err := s.Recompute(ctx)
	return ImportResult{Stats: st, Snapshot: snap}, err
}

func (s *Service) Recompute(ctx context.Context) (SnapshotResult, error) {
	snap, stored, err := service.Recompute(ctx, s.Store, s.Method, s.now(), s.SpeedWindow)
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{Stored: stored, Rows: len(snap.Rows), MethodVersion: snap.MethodVersion, ComputedAt: snap.ComputedAt}, nil
}
