package ports

import (
	"context"
	"errors"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
)

var (
	ErrNotFound = errors.New("repository record not found")
	ErrConflict = errors.New("repository optimistic lock conflict")
)

// LedgerRepository persists append-only entries. Implementations must never
// update or delete an existing entry.
type LedgerRepository interface {
	FindByIdempotency(ctx context.Context, operation ledger.Operation, key string) (*ledger.Entry, error)
	FindBySource(ctx context.Context, sourceType, sourceRef string, asset ledger.AssetType) (*ledger.Entry, error)
	Append(ctx context.Context, entry ledger.Entry) (ledger.Entry, error)
	ListAccountEntries(ctx context.Context, userID, periodID uint64, asset ledger.AssetType) ([]ledger.Entry, error)
}

// AccountRepository stores the rebuildable account snapshot. The returned
// account is locked until the surrounding UnitOfWork commits or rolls back.
type AccountRepository interface {
	GetOrCreateForUpdate(ctx context.Context, userID, periodID uint64, asset ledger.AssetType) (ledger.Account, error)
	Save(ctx context.Context, account ledger.Account) error
	ReplaceFromLedger(ctx context.Context, previousVersion uint64, rebuilt ledger.Account) error
	ListForUser(ctx context.Context, userID uint64) ([]ledger.Account, error)
	ListAll(ctx context.Context) ([]ledger.Account, error)
}
