package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
)

func TestProfileSummaryIsReadOnlyAndIncludesCurrentLedger(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Key: "2026-01", Status: period.StatusActive, StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour), Timezone: "Asia/Shanghai", ConfigVersion: "v1"}}
	store.accounts[accountKey(9, 3, ledger.AssetContribution)] = ledger.Account{ID: 1, UserID: 9, PeriodID: 3, AssetType: ledger.AssetContribution, Balance: 500}
	store.accounts[accountKey(9, 4, ledger.AssetContribution)] = ledger.Account{ID: 2, UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Balance: 1500}
	store.accounts[accountKey(9, 4, ledger.AssetTicket)] = ledger.Account{ID: 3, UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Balance: 1}
	store.entries = []ledger.Entry{
		{ID: 2, UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketMint, Amount: 1, BalanceAfter: 1, SourceType: "usage", SourceRef: "ticket:1"},
		{ID: 1, UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 1500, BalanceAfter: 1500, SourceType: "usage", SourceRef: "usage:1"},
	}
	profile, err := NewProfileService(memoryUnit{store: store}, []level.Definition{{Key: "new", Name: "新用户"}, {Key: "pulse", Name: "脉冲者", MinContributionMilli: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	before := len(store.entries)
	summary, err := profile.GetSummary(context.Background(), 9, at)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Profile.LifetimeContribution != 2000 || summary.Profile.Level.Key != "pulse" || summary.CurrentContribution != 1500 || summary.AvailableTickets != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(summary.CurrentLedgerEntries) != 2 || summary.CurrentLedgerEntries[0].ID != 1 || len(store.entries) != before {
		t.Fatalf("ledger=%+v entries=%d", summary.CurrentLedgerEntries, len(store.entries))
	}
}

func TestProfileSummaryBoundsRecentLedgerWithoutChangingBalances(t *testing.T) {
	for _, count := range []int{100, 101, 350} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := newMemoryLedgerStore()
			at := time.Unix(1_700_000_000, 0).UTC()
			store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
			store.accounts[accountKey(9, 3, ledger.AssetContribution)] = ledger.Account{UserID: 9, PeriodID: 3, AssetType: ledger.AssetContribution, Balance: 500}
			store.accounts[accountKey(9, 4, ledger.AssetContribution)] = ledger.Account{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Balance: 9000}
			store.accounts[accountKey(9, 4, ledger.AssetTicket)] = ledger.Account{UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Balance: 7}
			for i := count; i > 0; i-- {
				asset := ledger.AssetContribution
				if i%3 == 0 {
					asset = ledger.AssetTicket
				}
				store.entries = append(store.entries, ledger.Entry{ID: uint64(i), UserID: 9, PeriodID: 4, AssetType: asset, Amount: 1})
			}
			store.entries = append(store.entries,
				ledger.Entry{ID: 1000, UserID: 10, PeriodID: 4, AssetType: ledger.AssetContribution},
				ledger.Entry{ID: 1001, UserID: 9, PeriodID: 3, AssetType: ledger.AssetTicket})
			profile, err := NewProfileService(memoryUnit{store: store}, nil)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := profile.GetSummary(context.Background(), 9, at)
			if err != nil {
				t.Fatal(err)
			}
			if len(summary.CurrentLedgerEntries) != 100 || summary.LedgerHasMore != (count > 100) {
				t.Fatalf("entries=%d has_more=%v", len(summary.CurrentLedgerEntries), summary.LedgerHasMore)
			}
			for i, entry := range summary.CurrentLedgerEntries {
				if entry.ID != uint64(count-99+i) || entry.UserID != 9 || entry.PeriodID != 4 {
					t.Fatalf("entry %d = %+v", i, entry)
				}
			}
			if summary.Profile.LifetimeContribution != 9500 || summary.CurrentContribution != 9000 || summary.AvailableTickets != 7 || len(store.entries) != count+2 {
				t.Fatalf("summary changed balances or facts: %+v", summary)
			}
		})
	}
}

func TestProfileSummaryAllowsNoActivePeriod(t *testing.T) {
	store := newMemoryLedgerStore()
	store.accounts[accountKey(9, 3, ledger.AssetContribution)] = ledger.Account{ID: 1, UserID: 9, PeriodID: 3, AssetType: ledger.AssetContribution, Balance: 500}
	profile, err := NewProfileService(memoryUnit{store: store}, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := profile.GetSummary(context.Background(), 9, time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.CurrentPeriod != nil || summary.Profile.LifetimeContribution != 500 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestProfileSummaryClampsTicketDebtForProductView(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Key: "2026-01", Status: period.StatusActive, StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour), Timezone: "Asia/Shanghai", ConfigVersion: "v1"}}
	store.accounts[accountKey(9, 4, ledger.AssetTicket)] = ledger.Account{ID: 3, UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Balance: -2}

	profile, err := NewProfileService(memoryUnit{store: store}, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := profile.GetSummary(context.Background(), 9, at)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AvailableTickets != 0 {
		t.Fatalf("available tickets=%d, want 0 for ticket debt", summary.AvailableTickets)
	}
}
