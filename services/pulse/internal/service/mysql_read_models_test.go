package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

func TestMySQLRecentLedgerIsBoundedOrderedAndAccountScoped(t *testing.T) {
	database, _ := openMySQLIntegration(t)
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seed := uint64(time.Now().UnixNano())
	if err := unit.Do(ctx, func(repos ports.Repositories) error {
		var wantIDs []uint64
		for i := 0; i < 23; i++ {
			userID, periodID, asset := seed, seed, ledger.AssetContribution
			switch i {
			case 20:
				userID++
			case 21:
				periodID++
			case 22:
				asset = ledger.AssetTicket
			}
			operation := ledger.OperationContributionAdjustment
			if asset == ledger.AssetTicket {
				operation = ledger.OperationTicketAdjustment
			}
			entry, err := repos.Ledger.Append(ctx, ledger.Entry{
				UserID: userID, PeriodID: periodID, AssetType: asset,
				Operation: operation, Amount: 1, BalanceAfter: int64(i + 1),
				SourceType: "integration", SourceRef: fmt.Sprintf("recent:%d:%d", seed, i),
				IdempotencyKey: fmt.Sprintf("recent:%d:%d", seed, i), PayloadHash: "fixture", CreatedAt: time.Now(),
			})
			if err != nil {
				return err
			}
			if i < 20 {
				wantIDs = append(wantIDs, entry.ID)
			}
		}
		entries, err := repos.Ledger.ListRecentAccountEntries(ctx, seed, seed, ledger.AssetContribution, 5)
		if err != nil {
			return err
		}
		if len(entries) != 5 {
			t.Fatalf("recent entries=%d, want 5", len(entries))
		}
		for i, entry := range entries {
			if entry.ID != wantIDs[19-i] {
				t.Fatalf("recent entry %d ID=%d, want %d", i, entry.ID, wantIDs[19-i])
			}
		}
		if _, err := repos.Ledger.ListRecentAccountEntries(ctx, seed, seed, ledger.AssetContribution, 0); err == nil {
			t.Fatal("unbounded recent lookup accepted")
		}
		all, err := repos.Ledger.ListAccountEntries(ctx, seed, seed, ledger.AssetContribution)
		if err != nil {
			return err
		}
		if len(all) != 20 || all[0].ID != wantIDs[0] || all[19].ID != wantIDs[19] {
			t.Fatal("full reconstruction lookup was changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLReconciliationIncludesBalanceVersionAndEmptyAccounts(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	seed := uint64(now.UnixNano())
	readMismatches := func() int64 {
		t.Helper()
		var count int64
		if err := unit.Do(ctx, func(repos ports.Repositories) error {
			snapshot, err := repos.Operations.Snapshot(ctx, now, "integration", "integration")
			count = snapshot.LedgerMismatchCount
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := readMismatches()
	fixtures := []struct {
		user, period uint64
		asset        ledger.AssetType
		balance      int64
		version      uint64
		amounts      []int64
	}{
		{seed, seed, ledger.AssetContribution, 0, 0, nil},
		{seed + 1, seed, ledger.AssetContribution, 5, 0, nil}, // missing balance
		{seed + 2, seed, ledger.AssetContribution, 0, 1, nil}, // missing version
		{seed + 3, seed, ledger.AssetContribution, 0, 2, []int64{5, -5}},
		{seed + 4, seed, ledger.AssetContribution, 7, 2, []int64{7}},  // wrong version
		{seed + 5, seed, ledger.AssetContribution, 9, 1, []int64{10}}, // wrong balance
		{seed + 3, seed + 1, ledger.AssetContribution, 11, 1, []int64{11}},
		{seed + 3, seed, ledger.AssetTicket, 3, 1, []int64{3}},
	}
	for i, f := range fixtures {
		if _, err := db.ExecContext(ctx, `INSERT INTO pulse_account (user_id, period_id, asset_type, balance, version) VALUES (?, ?, ?, ?, ?)`, f.user, f.period, f.asset, f.balance, f.version); err != nil {
			t.Fatal(err)
		}
		for j, amount := range f.amounts {
			operation := ledger.OperationContributionAdjustment
			if f.asset == ledger.AssetTicket {
				operation = ledger.OperationTicketAdjustment
			}
			if err := unit.Do(ctx, func(repos ports.Repositories) error {
				_, err := repos.Ledger.Append(ctx, ledger.Entry{
					UserID: f.user, PeriodID: f.period, AssetType: f.asset, Operation: operation,
					Amount: amount, BalanceAfter: f.balance, SourceType: "integration",
					SourceRef:      fmt.Sprintf("reconcile:%d:%d:%d", seed, i, j),
					IdempotencyKey: fmt.Sprintf("reconcile:%d:%d:%d", seed, i, j), PayloadHash: "fixture", CreatedAt: now,
				})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := readMismatches() - before; got != 4 {
		t.Fatalf("new mismatches=%d, want 4", got)
	}
	// The diagnostic must remain read-only even for inconsistent accounts.
	var balance int64
	if err := db.QueryRowContext(ctx, `SELECT balance FROM pulse_account WHERE user_id=? AND period_id=? AND asset_type=?`, seed+5, seed, ledger.AssetContribution).Scan(&balance); err != nil || balance != 9 {
		t.Fatalf("diagnostic changed account: balance=%d err=%v", balance, err)
	}
}
