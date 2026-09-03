// Package service contains transaction orchestration for Pulse use cases.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// LedgerService appends one accounting entry and updates the derived account
// in the same Pulse database transaction.
type LedgerService struct {
	unit ports.UnitOfWork
}

func NewLedgerService(unit ports.UnitOfWork) (*LedgerService, error) {
	if unit == nil {
		return nil, errors.New("ledger unit of work is nil")
	}
	return &LedgerService{unit: unit}, nil
}

func (s *LedgerService) Append(ctx context.Context, requested ledger.Entry) (ledger.Entry, error) {
	if err := ledger.ValidateEntry(requested); err != nil {
		return ledger.Entry{}, err
	}
	var result ledger.Entry
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		var err error
		result, err = appendEntry(ctx, repos, requested)
		return err
	})
	if err != nil {
		return ledger.Entry{}, err
	}
	return result, nil
}

// appendEntry is the transaction-local primitive shared by direct ledger
// mutations and Usage Ingest. It deliberately does not start a transaction.
func appendEntry(ctx context.Context, repos ports.Repositories, requested ledger.Entry) (ledger.Entry, error) {
	if repos.Ledger == nil || repos.Account == nil {
		return ledger.Entry{}, errors.New("ledger repositories are not initialized")
	}
	if err := ledger.ValidateEntry(requested); err != nil {
		return ledger.Entry{}, err
	}

	// Check before locking the account for the common replay path. The second
	// check after locking closes the race with another writer of this account.
	existing, err := repos.Ledger.FindByIdempotency(ctx, requested.Operation, requested.IdempotencyKey)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return ledger.Entry{}, err
	}
	if existing != nil {
		if err := reuseOrConflict(requested, *existing); err != nil {
			return ledger.Entry{}, err
		}
		return *existing, nil
	}

	account, err := repos.Account.GetOrCreateForUpdate(ctx, requested.UserID, requested.PeriodID, requested.AssetType)
	if err != nil {
		return ledger.Entry{}, err
	}
	existing, err = repos.Ledger.FindByIdempotency(ctx, requested.Operation, requested.IdempotencyKey)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return ledger.Entry{}, err
	}
	if existing != nil {
		if err := reuseOrConflict(requested, *existing); err != nil {
			return ledger.Entry{}, err
		}
		return *existing, nil
	}

	next, err := ledger.Apply(account, requested)
	if err != nil {
		return ledger.Entry{}, err
	}
	requested.BalanceAfter = next.Balance
	persisted, err := repos.Ledger.Append(ctx, requested)
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("append ledger entry: %w", err)
	}
	if err := repos.Account.Save(ctx, next); err != nil {
		return ledger.Entry{}, fmt.Errorf("save account snapshot: %w", err)
	}
	return persisted, nil
}

// RebuildAccount reconstructs one account exclusively from append-only entries
// and repairs its derived snapshot in the same transaction.
func (s *LedgerService) RebuildAccount(ctx context.Context, userID, periodID uint64, asset ledger.AssetType) (ledger.Account, error) {
	if userID == 0 || periodID == 0 || (asset != ledger.AssetContribution && asset != ledger.AssetTicket) {
		return ledger.Account{}, fmt.Errorf("%w: invalid account identity", ledger.ErrInvalidEntry)
	}
	var result ledger.Account
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Ledger == nil || repos.Account == nil {
			return errors.New("ledger repositories are not initialized")
		}
		current, err := repos.Account.GetOrCreateForUpdate(ctx, userID, periodID, asset)
		if err != nil {
			return err
		}
		entries, err := repos.Ledger.ListAccountEntries(ctx, userID, periodID, asset)
		if err != nil {
			return err
		}
		rebuilt, err := ledger.Rebuild(ledger.Account{ID: current.ID, UserID: userID, PeriodID: periodID, AssetType: asset}, entries)
		if err != nil {
			return err
		}
		if rebuilt.Balance != current.Balance || rebuilt.Version != current.Version {
			if err := repos.Account.ReplaceFromLedger(ctx, current.Version, rebuilt); err != nil {
				return fmt.Errorf("replace account snapshot: %w", err)
			}
		}
		result = rebuilt
		return nil
	})
	if err != nil {
		return ledger.Account{}, err
	}
	return result, nil
}

func reuseOrConflict(requested, existing ledger.Entry) error {
	if requested.PayloadHash != existing.PayloadHash {
		return fmt.Errorf("%w: operation=%s key=%s", ledger.ErrIdempotencyConflict, requested.Operation, requested.IdempotencyKey)
	}
	return nil
}
