package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// Explicit pre-upgrade fixture: production ingestion must never mint these now.
func seedLegacyTicketLot(t *testing.T, unit ports.UnitOfWork, user, periodID uint64, key string, count int64, earned time.Time, days int) {
	t.Helper()
	err := unit.Do(context.Background(), func(r ports.Repositories) error {
		if err := r.Tickets.LockUser(context.Background(), user); err != nil {
			return err
		}
		expires := earned.Add(time.Duration(days) * 24 * time.Hour)
		meta, _ := json.Marshal(map[string]int64{"ticket_lot_version": 1, "earned_at": earned.Unix(), "quota_expires_at": expires.Unix()})
		entry, err := appendEntry(context.Background(), r, ledger.Entry{UserID: user, PeriodID: periodID, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketMint, Amount: count, SourceType: "usage", SourceRef: key, IdempotencyKey: key, PayloadHash: fmt.Sprintf("%064x", count), MetadataJSON: meta})
		if err != nil {
			return err
		}
		return r.Tickets.Mint(context.Background(), ports.TicketLot{UserID: user, PeriodID: periodID, MintEntryID: entry.ID, Issued: count, Remaining: count, EarnedAt: earned, QuotaExpiresAt: expires})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUsageNeverChangesHistoricalOrCommunityTickets(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(fmt.Sprintf("continuous=%v", continuous), func(t *testing.T) {
			ctx := context.Background()
			store := newMemoryLedgerStore()
			store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", Continuous: continuous, StartsAt: time.Unix(1_600_000_000, 0), EndsAt: time.Unix(1_800_000_000, 0), TicketThresholdMilli: 1}}
			store.rules[4] = []economics.Rule{{ID: 8, Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
			unit := memoryUnit{store: store}
			// Preserve both pre-upgrade paid tickets and independently earned bonuses.
			for i, op := range []ledger.Operation{ledger.OperationTicketMint, ledger.OperationTicketBonus} {
				if err := unit.Do(ctx, func(r ports.Repositories) error {
					_, err := appendEntry(ctx, r, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Operation: op, Amount: 2, SourceType: "fixture", SourceRef: fmt.Sprint(i), IdempotencyKey: fmt.Sprint(i), PayloadHash: "fixture"})
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			stat, _ := store.GetStat(ctx, 9, 4)
			stat.EntitledTickets = 2
			store.SaveStat(ctx, stat)
			consume := ingestEvent("new-consume", "new-hash", usage.EventConsume, 1_000_000)
			ingest, err := NewUsageIngestService(unit, staticUsageSource{}, UsageIngestConfig{BatchSize: 100})
			if err != nil {
				t.Fatal(err)
			} // No threshold is required to ingest usage.
			var result IngestResult
			for i := 0; i < 100; i++ {
				if err := ingest.processOne(ctx, consume, &result); err != nil {
					t.Fatal(err)
				}
			}
			refund := ingestEvent("new-refund", "refund-hash", usage.EventRefund, -1_000_000)
			refund.RelatedSourceEventID = consume.SourceEventID
			if err := ingest.processOne(ctx, refund, &result); err != nil {
				t.Fatal(err)
			}
			if result.Accepted != 2 || result.Replayed != 99 || result.TicketsMinted != 0 || result.TicketsReversed != 0 {
				t.Fatalf("result=%+v", result)
			}
			if len(store.entries) != 4 || store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance != 4 {
				t.Fatalf("changed tickets: %+v", store.entries)
			}
			got, _ := store.GetStat(ctx, 9, 4)
			if got.EntitledTickets != 2 || got.NetContributionMilli != 0 || got.UsageEventCount != 2 {
				t.Fatalf("stat=%+v", got)
			}
			// Backfill older source rows must also remain contribution-only.
			back := ingestEvent("backfill", "backfill-hash", usage.EventConsume, 500_000)
			// The memory fixture has one cursor slot; model a fresh backfill cursor.
			store.cursor = ports.Cursor{}
			source := cursorAwareSource{events: []usage.Event{back}}
			backfill, _ := NewBackfillService(ingest, source)
			report, err := backfill.Run(ctx, BackfillOptions{From: back.SourceCreatedAt.Add(-time.Hour), To: back.SourceCreatedAt.Add(time.Hour)})
			if err != nil || report.Accepted != 1 || store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance != 4 {
				t.Fatalf("backfill=%+v err=%v", report, err)
			}
		})
	}
}
