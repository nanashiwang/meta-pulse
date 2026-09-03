package mysql

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"gorm.io/gorm"
)

var ErrLedgerMismatch = errors.New("ledger and account mismatch")

type LedgerCheckReport struct {
	AccountsChecked int      `json:"accounts_checked"`
	EntriesChecked  int      `json:"entries_checked"`
	MismatchCount   int      `json:"mismatch_count"`
	Mismatches      []string `json:"mismatches,omitempty"`
}

type orphanLedgerGroup struct {
	UserID     uint64 `gorm:"column:user_id"`
	PeriodID   uint64 `gorm:"column:period_id"`
	AssetType  string `gorm:"column:asset_type"`
	EntryCount int64  `gorm:"column:entry_count"`
}

// CheckLedger runs from one repeatable-read transaction so a concurrent
// append cannot create a false mismatch between ledger and account.
func (db *DB) CheckLedger(ctx context.Context) (LedgerCheckReport, error) {
	if db == nil || db.gorm == nil {
		return LedgerCheckReport{}, errors.New("pulse database is not initialized")
	}
	var report LedgerCheckReport
	err := db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repos := newRepositories(tx)
		accounts, err := repos.Account.ListAll(ctx)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			entries, err := repos.Ledger.ListAccountEntries(ctx, account.UserID, account.PeriodID, account.AssetType)
			if err != nil {
				return err
			}
			report.AccountsChecked++
			report.EntriesChecked += len(entries)
			rebuilt, rebuildErr := ledger.Rebuild(ledger.Account{ID: account.ID, UserID: account.UserID, PeriodID: account.PeriodID, AssetType: account.AssetType}, entries)
			if rebuildErr != nil {
				report.Mismatches = append(report.Mismatches, fmt.Sprintf("account %d: %v", account.ID, rebuildErr))
				continue
			}
			if rebuilt.Balance != account.Balance || rebuilt.Version != account.Version {
				report.Mismatches = append(report.Mismatches, fmt.Sprintf("account %d: snapshot balance/version=%d/%d ledger=%d/%d", account.ID, account.Balance, account.Version, rebuilt.Balance, rebuilt.Version))
			}
		}

		var orphans []orphanLedgerGroup
		if err := tx.Raw(`
SELECT l.user_id, l.period_id, l.asset_type, COUNT(*) AS entry_count
FROM pulse_ledger_entry l
LEFT JOIN pulse_account a
  ON a.user_id = l.user_id AND a.period_id = l.period_id AND a.asset_type = l.asset_type
WHERE a.id IS NULL
GROUP BY l.user_id, l.period_id, l.asset_type`).Scan(&orphans).Error; err != nil {
			return fmt.Errorf("find orphan ledger groups: %w", err)
		}
		for _, orphan := range orphans {
			report.EntriesChecked += int(orphan.EntryCount)
			report.Mismatches = append(report.Mismatches, fmt.Sprintf("missing account: user=%d period=%d asset=%s entries=%d", orphan.UserID, orphan.PeriodID, orphan.AssetType, orphan.EntryCount))
		}
		return nil
	})
	report.MismatchCount = len(report.Mismatches)
	if err != nil {
		return report, err
	}
	if report.MismatchCount > 0 {
		return report, ErrLedgerMismatch
	}
	return report, nil
}
