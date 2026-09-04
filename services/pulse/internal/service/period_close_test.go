package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type periodAdminMemory struct {
	periods           []period.Period
	transitions       []string
	failClosingOnce   bool
	closeBeforeReRead bool
}

func (m *periodAdminMemory) ListDueForClose(_ context.Context, now time.Time, limit int) ([]period.Period, error) {
	var result []period.Period
	for _, candidate := range m.periods {
		if (candidate.Status == period.StatusActive || candidate.Status == period.StatusSettling) && !candidate.EndsAt.After(now) {
			result = append(result, candidate)
			if len(result) == limit {
				break
			}
		}
	}
	if m.closeBeforeReRead && len(result) > 0 {
		m.closeBeforeReRead = false
		for i := range m.periods {
			if m.periods[i].ID == result[0].ID {
				m.periods[i].Status = period.StatusClosed
			}
		}
	}
	return result, nil
}

func (m *periodAdminMemory) FindByIDForUpdate(_ context.Context, periodID uint64) (period.Period, error) {
	for _, candidate := range m.periods {
		if candidate.ID == periodID {
			return candidate, nil
		}
	}
	return period.Period{}, ports.ErrNotFound
}

func (m *periodAdminMemory) Transition(_ context.Context, periodID uint64, from, to period.Status, _ time.Time) error {
	if m.failClosingOnce && to == period.StatusClosed {
		m.failClosingOnce = false
		return errors.New("simulated close interruption")
	}
	for i := range m.periods {
		if m.periods[i].ID == periodID && m.periods[i].Status == from {
			if err := period.Transition(from, to); err != nil {
				return err
			}
			m.periods[i].Status = to
			m.transitions = append(m.transitions, string(from)+"->"+string(to))
			return nil
		}
	}
	return ports.ErrConflict
}

type periodCloseUnit struct {
	ledger *memoryLedgerStore
	reward *memoryRewardStore
	admin  *periodAdminMemory
}

func (u periodCloseUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{
		Ledger: u.ledger, Account: u.ledger, Cursor: memoryCursorRepo{u.ledger},
		Reward: u.reward, PeriodAdmin: u.admin,
	})
}

func setupPeriodClose(t *testing.T, enableRewards bool) (*PeriodCloseService, *memoryLedgerStore, *memoryRewardStore, *periodAdminMemory, time.Time) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	activity := period.Period{ID: 4, Key: "p4", Status: period.StatusActive, StartsAt: now.Add(-48 * time.Hour), EndsAt: now.Add(-time.Hour), ConfigVersion: "v1", RandomVersion: "hmac-v1"}
	ledgerStore := newMemoryLedgerStore()
	watermark := activity.EndsAt
	ledgerStore.cursor = ports.Cursor{Name: DefaultUsageCursorName, SourceSystem: "new-api-log", WatermarkAt: &watermark}
	rewardStore := newMemoryRewardStore()
	rewardStore.definitions = []reward.Definition{{ID: 8, RewardKey: "quota", RewardType: "quota", Amount: 10, Weight: 1, ConfigVersion: "v1", Enabled: true}}
	rewardStore.budgets[budgetKey(activity.ID, "period_reward")] = ports.RewardBudget{ID: 2, PeriodID: activity.ID, BudgetType: "period_reward", HardCap: 20}
	admin := &periodAdminMemory{periods: []period.Period{activity}}
	service, err := NewPeriodCloseService(periodCloseUnit{ledger: ledgerStore, reward: rewardStore, admin: admin}, PeriodCloseConfig{
		BatchSize: 10, RequireWatermark: true, EnablePeriodRewards: enableRewards,
		RandomSecret: []byte("period-close-secret"), ShadowMode: true, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, ledgerStore, rewardStore, admin, now
}

func appendCloseAccountEntry(t *testing.T, store *memoryLedgerStore, entry ledger.Entry) {
	t.Helper()
	s, err := NewLedgerService(memoryUnit{store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
}

func TestPeriodCloseIsReentrantAndUsesStableRewardAction(t *testing.T) {
	closer, ledgerStore, rewardStore, admin, now := setupPeriodClose(t, true)
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 2000, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "contribution:event:1", PayloadHash: "hash-1", CreatedAt: now})
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketMint, Amount: 2, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "ticket:event:1", PayloadHash: "hash-1", CreatedAt: now})

	first, err := closer.RunOnce(context.Background())
	if err != nil || first.Closed != 1 || first.TicketsExpired != 1 || first.RewardsCreated != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if got := ledgerStore.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance; got != 0 {
		t.Fatalf("ticket balance=%d, want 0", got)
	}
	if len(rewardStore.grants) != 1 || len(rewardStore.outboxes) != 1 || rewardStore.grants[0].ActionID != "period_reward:4:9" || rewardStore.outboxes[0].Status != OutboxStatusShadow {
		t.Fatalf("grants=%+v outboxes=%+v", rewardStore.grants, rewardStore.outboxes)
	}
	if got := rewardStore.budgets[budgetKey(4, "period_reward")].ReservedAmount; got != 10 {
		t.Fatalf("reserved=%d, want 10", got)
	}
	second, err := closer.RunOnce(context.Background())
	if err != nil || second.Checked != 0 || len(rewardStore.grants) != 1 || len(ledgerStore.entries) != 3 {
		t.Fatalf("second=%+v err=%v grants=%d entries=%d", second, err, len(rewardStore.grants), len(ledgerStore.entries))
	}
	if admin.periods[0].Status != period.StatusClosed || strings.Join(admin.transitions, ",") != "active->settling,settling->closed" {
		t.Fatalf("period=%+v transitions=%v", admin.periods[0], admin.transitions)
	}
}

func TestPeriodCloseDefersBeforeChangingActiveStateWhenWatermarkLags(t *testing.T) {
	closer, ledgerStore, _, admin, _ := setupPeriodClose(t, false)
	lagging := admin.periods[0].EndsAt.Add(-time.Second)
	ledgerStore.cursor.WatermarkAt = &lagging
	report, err := closer.RunOnce(context.Background())
	if err != nil || report.Deferred != 1 || report.Closed != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if admin.periods[0].Status != period.StatusActive || len(admin.transitions) != 0 {
		t.Fatalf("period=%+v transitions=%v", admin.periods[0], admin.transitions)
	}
}

func TestPeriodCloseMismatchFailsWithoutClosing(t *testing.T) {
	closer, ledgerStore, _, admin, now := setupPeriodClose(t, false)
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 1000, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "contribution:event:1", PayloadHash: "hash-1", CreatedAt: now})
	account := ledgerStore.accounts[accountKey(9, 4, ledger.AssetContribution)]
	account.Balance++
	ledgerStore.accounts[accountKey(9, 4, ledger.AssetContribution)] = account
	report, err := closer.RunOnce(context.Background())
	if err != nil || report.Failed != 1 || report.Closed != 0 || admin.periods[0].Status != period.StatusSettling {
		t.Fatalf("report=%+v err=%v period=%+v", report, err, admin.periods[0])
	}
}

func TestPeriodCloseResumesAfterInterruptionWithoutDuplicateEffects(t *testing.T) {
	closer, ledgerStore, rewardStore, admin, now := setupPeriodClose(t, true)
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 1000, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "contribution:event:1", PayloadHash: "hash-1", CreatedAt: now})
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketMint, Amount: 1, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "ticket:event:1", PayloadHash: "hash-1", CreatedAt: now})
	admin.failClosingOnce = true
	first, err := closer.RunOnce(context.Background())
	if err != nil || first.Failed != 1 || admin.periods[0].Status != period.StatusSettling {
		t.Fatalf("first=%+v err=%v period=%+v", first, err, admin.periods[0])
	}
	second, err := closer.RunOnce(context.Background())
	if err != nil || second.Closed != 1 || second.RewardsCreated != 0 || second.TicketsExpired != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if len(rewardStore.grants) != 1 || len(rewardStore.outboxes) != 1 || len(ledgerStore.entries) != 3 {
		t.Fatalf("grants=%d outboxes=%d entries=%d", len(rewardStore.grants), len(rewardStore.outboxes), len(ledgerStore.entries))
	}
}

func TestPeriodCloseHardBudgetPreventsAnyReward(t *testing.T) {
	closer, ledgerStore, rewardStore, admin, now := setupPeriodClose(t, true)
	rewardStore.budgets[budgetKey(4, "period_reward")] = ports.RewardBudget{ID: 2, PeriodID: 4, BudgetType: "period_reward", HardCap: 5}
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 1000, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "contribution:event:1", PayloadHash: "hash-1", CreatedAt: now})
	report, err := closer.RunOnce(context.Background())
	if err != nil || report.Failed != 1 || len(rewardStore.grants) != 0 || admin.periods[0].Status != period.StatusSettling {
		t.Fatalf("report=%+v err=%v grants=%d period=%+v", report, err, len(rewardStore.grants), admin.periods[0])
	}
}

func TestPeriodCloseSkipsPeriodClosedAfterListing(t *testing.T) {
	closer, _, _, admin, _ := setupPeriodClose(t, false)
	admin.closeBeforeReRead = true
	report, err := closer.RunOnce(context.Background())
	if err != nil || report.Checked != 1 || report.Closed != 0 || report.Failed != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if admin.periods[0].Status != period.StatusClosed || len(admin.transitions) != 0 {
		t.Fatalf("period=%+v transitions=%v", admin.periods[0], admin.transitions)
	}
}

func TestPeriodCloseFailsClosedForInvalidRewardTable(t *testing.T) {
	closer, ledgerStore, rewardStore, admin, now := setupPeriodClose(t, true)
	rewardStore.definitions = append(rewardStore.definitions, reward.Definition{ID: 9, RewardKey: "wrong-version", RewardType: "quota", Amount: 10, Weight: 1, ConfigVersion: "v2", Enabled: true})
	appendCloseAccountEntry(t, ledgerStore, ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 1000, SourceType: "usage", SourceRef: "event:1", IdempotencyKey: "contribution:event:1", PayloadHash: "hash-1", CreatedAt: now})
	report, err := closer.RunOnce(context.Background())
	if err != nil || report.Failed != 1 || report.Closed != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if len(rewardStore.grants) != 0 || len(rewardStore.outboxes) != 0 || rewardStore.budgets[budgetKey(4, "period_reward")].ReservedAmount != 0 {
		t.Fatalf("invalid table created rewards grants=%d outboxes=%d budget=%+v", len(rewardStore.grants), len(rewardStore.outboxes), rewardStore.budgets[budgetKey(4, "period_reward")])
	}
	if admin.periods[0].Status != period.StatusSettling {
		t.Fatalf("memory fixture did not reach validation path: period=%+v", admin.periods[0])
	}
}
