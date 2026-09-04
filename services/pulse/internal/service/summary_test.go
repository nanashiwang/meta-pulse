package service

import (
	"context"
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
