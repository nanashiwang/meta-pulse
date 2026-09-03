package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ledgerRepository struct{ db *gorm.DB }
type accountRepository struct{ db *gorm.DB }

type ledgerEntryModel struct {
	ID                uint64    `gorm:"column:id;primaryKey"`
	UserID            uint64    `gorm:"column:user_id"`
	PeriodID          uint64    `gorm:"column:period_id"`
	AssetType         string    `gorm:"column:asset_type"`
	Operation         string    `gorm:"column:operation"`
	Amount            int64     `gorm:"column:amount"`
	BalanceAfter      int64     `gorm:"column:balance_after"`
	SourceType        string    `gorm:"column:source_type"`
	SourceRef         string    `gorm:"column:source_ref"`
	IdempotencyKey    string    `gorm:"column:idempotency_key"`
	PayloadHash       string    `gorm:"column:payload_hash"`
	ReversalOfEntryID *uint64   `gorm:"column:reversal_of_entry_id"`
	Reason            string    `gorm:"column:reason"`
	MetadataJSON      []byte    `gorm:"column:metadata_json;type:json"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (ledgerEntryModel) TableName() string { return "pulse_ledger_entry" }

type accountModel struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	UserID    uint64    `gorm:"column:user_id"`
	PeriodID  uint64    `gorm:"column:period_id"`
	AssetType string    `gorm:"column:asset_type"`
	Balance   int64     `gorm:"column:balance"`
	Version   uint64    `gorm:"column:version"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (accountModel) TableName() string { return "pulse_account" }

func newRepositories(db *gorm.DB) ports.Repositories {
	return ports.Repositories{
		Ledger:  &ledgerRepository{db: db},
		Account: &accountRepository{db: db},
	}
}

func (r *ledgerRepository) FindByIdempotency(ctx context.Context, operation ledger.Operation, key string) (*ledger.Entry, error) {
	var model ledgerEntryModel
	err := r.db.WithContext(ctx).Where("operation = ? AND idempotency_key = ?", operation, key).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find ledger idempotency: %w", err)
	}
	entry := model.toDomain()
	return &entry, nil
}

func (r *ledgerRepository) Append(ctx context.Context, entry ledger.Entry) (ledger.Entry, error) {
	model := ledgerModelFromDomain(entry)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ledger.Entry{}, ports.ErrConflict
		}
		return ledger.Entry{}, fmt.Errorf("create ledger entry: %w", err)
	}
	return model.toDomain(), nil
}

func (r *ledgerRepository) ListAccountEntries(ctx context.Context, userID, periodID uint64, asset ledger.AssetType) ([]ledger.Entry, error) {
	var models []ledgerEntryModel
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND period_id = ? AND asset_type = ?", userID, periodID, asset).
		Order("id ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list account ledger entries: %w", err)
	}
	entries := make([]ledger.Entry, len(models))
	for i := range models {
		entries[i] = models[i].toDomain()
	}
	return entries, nil
}

func (r *accountRepository) GetOrCreateForUpdate(ctx context.Context, userID, periodID uint64, asset ledger.AssetType) (ledger.Account, error) {
	// Atomic no-op upsert avoids concurrent first-write races; the subsequent
	// SELECT FOR UPDATE serializes every mutation of this account snapshot.
	if err := r.db.WithContext(ctx).Exec(`
INSERT INTO pulse_account (user_id, period_id, asset_type, balance, version)
VALUES (?, ?, ?, 0, 0)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, userID, periodID, asset).Error; err != nil {
		return ledger.Account{}, fmt.Errorf("ensure account: %w", err)
	}
	var model accountModel
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND period_id = ? AND asset_type = ?", userID, periodID, asset).
		Take(&model).Error; err != nil {
		return ledger.Account{}, fmt.Errorf("lock account: %w", err)
	}
	return model.toDomain(), nil
}

func (r *accountRepository) Save(ctx context.Context, account ledger.Account) error {
	if account.ID == 0 || account.Version == 0 {
		return fmt.Errorf("%w: invalid account version", ports.ErrConflict)
	}
	result := r.db.WithContext(ctx).Model(&accountModel{}).
		Where("id = ? AND version = ?", account.ID, account.Version-1).
		Updates(map[string]any{"balance": account.Balance, "version": account.Version})
	if result.Error != nil {
		return fmt.Errorf("update account: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *accountRepository) ReplaceFromLedger(ctx context.Context, previousVersion uint64, rebuilt ledger.Account) error {
	if rebuilt.ID == 0 {
		return fmt.Errorf("%w: invalid account identity", ports.ErrConflict)
	}
	result := r.db.WithContext(ctx).Model(&accountModel{}).
		Where("id = ? AND version = ?", rebuilt.ID, previousVersion).
		Updates(map[string]any{"balance": rebuilt.Balance, "version": rebuilt.Version})
	if result.Error != nil {
		return fmt.Errorf("rebuild account: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *accountRepository) ListAll(ctx context.Context) ([]ledger.Account, error) {
	var models []accountModel
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	accounts := make([]ledger.Account, len(models))
	for i := range models {
		accounts[i] = models[i].toDomain()
	}
	return accounts, nil
}

func ledgerModelFromDomain(entry ledger.Entry) ledgerEntryModel {
	return ledgerEntryModel{
		ID: entry.ID, UserID: entry.UserID, PeriodID: entry.PeriodID,
		AssetType: string(entry.AssetType), Operation: string(entry.Operation),
		Amount: entry.Amount, BalanceAfter: entry.BalanceAfter,
		SourceType: entry.SourceType, SourceRef: entry.SourceRef,
		IdempotencyKey: entry.IdempotencyKey, PayloadHash: entry.PayloadHash,
		ReversalOfEntryID: entry.ReversalOfEntryID, Reason: entry.Reason,
		MetadataJSON: entry.MetadataJSON, CreatedAt: entry.CreatedAt,
	}
}

func (m ledgerEntryModel) toDomain() ledger.Entry {
	return ledger.Entry{
		ID: m.ID, UserID: m.UserID, PeriodID: m.PeriodID,
		AssetType: ledger.AssetType(m.AssetType), Operation: ledger.Operation(m.Operation),
		Amount: m.Amount, BalanceAfter: m.BalanceAfter,
		SourceType: m.SourceType, SourceRef: m.SourceRef,
		IdempotencyKey: m.IdempotencyKey, PayloadHash: m.PayloadHash,
		ReversalOfEntryID: m.ReversalOfEntryID, Reason: m.Reason,
		MetadataJSON: m.MetadataJSON, CreatedAt: m.CreatedAt,
	}
}

func (m accountModel) toDomain() ledger.Account {
	return ledger.Account{ID: m.ID, UserID: m.UserID, PeriodID: m.PeriodID, AssetType: ledger.AssetType(m.AssetType), Balance: m.Balance, Version: m.Version}
}
