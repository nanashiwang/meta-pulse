// Package ledger contains append-only accounting primitives. It has no
// persistence or transport dependency.
package ledger

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type AssetType string
type Operation string

const (
	AssetContribution AssetType = "contribution"
	AssetTicket       AssetType = "ticket"

	OperationContributionEarn       Operation = "contribution_earn"
	OperationContributionReverse    Operation = "contribution_reverse"
	OperationContributionAdjustment Operation = "contribution_adjustment"
	OperationTicketMint             Operation = "ticket_mint"
	OperationTicketSpend            Operation = "ticket_spend"
	OperationTicketReverse          Operation = "ticket_reverse"
	OperationTicketExpire           Operation = "ticket_expire"
	OperationTicketAdjustment       Operation = "ticket_adjustment"
)

var (
	ErrInvalidEntry        = errors.New("invalid ledger entry")
	ErrIdempotencyConflict = errors.New("ledger idempotency conflict")
	ErrBalanceOverflow     = errors.New("ledger balance overflow")
)

type Entry struct {
	ID                uint64
	UserID            uint64
	PeriodID          uint64
	AssetType         AssetType
	Operation         Operation
	Amount            int64
	BalanceAfter      int64
	SourceType        string
	SourceRef         string
	IdempotencyKey    string
	PayloadHash       string
	ReversalOfEntryID *uint64
	Reason            string
	MetadataJSON      []byte
	CreatedAt         time.Time
}

type Account struct {
	ID        uint64
	UserID    uint64
	PeriodID  uint64
	AssetType AssetType
	Balance   int64
	Version   uint64
}

func ValidateEntry(entry Entry) error {
	if entry.UserID == 0 || entry.PeriodID == 0 || entry.AssetType == "" || entry.Operation == "" {
		return fmt.Errorf("%w: missing identity", ErrInvalidEntry)
	}
	if entry.Amount == 0 {
		return fmt.Errorf("%w: zero amount", ErrInvalidEntry)
	}
	if entry.SourceType == "" || entry.SourceRef == "" || entry.IdempotencyKey == "" || entry.PayloadHash == "" {
		return fmt.Errorf("%w: missing source or idempotency", ErrInvalidEntry)
	}
	switch entry.AssetType {
	case AssetContribution:
		switch entry.Operation {
		case OperationContributionEarn:
			if entry.Amount < 0 {
				return fmt.Errorf("%w: contribution earn must be positive", ErrInvalidEntry)
			}
		case OperationContributionReverse:
			if entry.Amount > 0 || entry.ReversalOfEntryID == nil {
				return fmt.Errorf("%w: contribution reverse must negate an entry", ErrInvalidEntry)
			}
		case OperationContributionAdjustment:
			if entry.Reason == "" {
				return fmt.Errorf("%w: adjustment requires reason", ErrInvalidEntry)
			}
		default:
			return fmt.Errorf("%w: contribution operation mismatch", ErrInvalidEntry)
		}
	case AssetTicket:
		switch entry.Operation {
		case OperationTicketMint:
			if entry.Amount < 0 {
				return fmt.Errorf("%w: ticket mint must be positive", ErrInvalidEntry)
			}
		case OperationTicketSpend, OperationTicketExpire:
			if entry.Amount > 0 {
				return fmt.Errorf("%w: ticket debit must be negative", ErrInvalidEntry)
			}
		case OperationTicketReverse:
			if entry.Amount < 0 || entry.ReversalOfEntryID == nil {
				return fmt.Errorf("%w: ticket reverse must restore an entry", ErrInvalidEntry)
			}
		case OperationTicketAdjustment:
			if entry.Reason == "" {
				return fmt.Errorf("%w: adjustment requires reason", ErrInvalidEntry)
			}
		default:
			return fmt.Errorf("%w: ticket operation mismatch", ErrInvalidEntry)
		}
	default:
		return fmt.Errorf("%w: unknown asset type", ErrInvalidEntry)
	}
	return nil
}

func Apply(account Account, entry Entry) (Account, error) {
	if account.UserID != entry.UserID || account.PeriodID != entry.PeriodID || account.AssetType != entry.AssetType {
		return Account{}, fmt.Errorf("%w: account does not match entry", ErrInvalidEntry)
	}
	if err := ValidateEntry(entry); err != nil {
		return Account{}, err
	}
	if entry.Amount > 0 && account.Balance > math.MaxInt64-entry.Amount {
		return Account{}, ErrBalanceOverflow
	}
	if entry.Amount < 0 && account.Balance < math.MinInt64-entry.Amount {
		return Account{}, ErrBalanceOverflow
	}
	account.Balance += entry.Amount
	if account.Version == math.MaxUint64 {
		return Account{}, ErrBalanceOverflow
	}
	account.Version++
	return account, nil
}

// Rebuild replays entries in append order and verifies every persisted
// balance_after. This catches both missing entries and tampered snapshots.
func Rebuild(initial Account, entries []Entry) (Account, error) {
	account := initial
	for index, entry := range entries {
		next, err := Apply(account, entry)
		if err != nil {
			return Account{}, fmt.Errorf("entry %d: %w", index, err)
		}
		if next.Balance != entry.BalanceAfter {
			return Account{}, fmt.Errorf("entry %d: %w: balance_after=%d calculated=%d", index, ErrInvalidEntry, entry.BalanceAfter, next.Balance)
		}
		account = next
	}
	return account, nil
}
