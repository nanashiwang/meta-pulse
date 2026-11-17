package service

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"testing"
	"time"
)

type flashMemoryRewards struct {
	*memoryRewardStore
	setup      *rewardAdminMemory
	failOutbox bool
}

func (m *flashMemoryRewards) ListDefinitions(context.Context, uint64) ([]reward.Definition, error) {
	return m.setup.definitions, nil
}
func (m *flashMemoryRewards) GetBudgetForUpdate(ctx context.Context, id uint64, kind string) (ports.RewardBudget, error) {
	if b, err := m.memoryRewardStore.GetBudgetForUpdate(ctx, id, kind); err == nil {
		return b, nil
	}
	for _, b := range m.setup.budgets {
		if b.PeriodID == id && b.BudgetType == kind {
			return b, nil
		}
	}
	return ports.RewardBudget{}, ports.ErrNotFound
}
func (m *flashMemoryRewards) CreateOutbox(ctx context.Context, b ports.SettlementOutbox) (ports.SettlementOutbox, error) {
	if m.failOutbox {
		return ports.SettlementOutbox{}, errors.New("outbox unavailable")
	}
	return m.memoryRewardStore.CreateOutbox(ctx, b)
}

type flashUnit struct {
	base    *periodCreateUnit
	rewards *flashMemoryRewards
}

func (u flashUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	grants := append([]ports.RewardGrant(nil), u.rewards.grants...)
	boxes := append([]ports.SettlementOutbox(nil), u.rewards.outboxes...)
	budgets := map[string]ports.RewardBudget{}
	for k, v := range u.rewards.budgets {
		budgets[k] = v
	}
	idem := map[string]ports.IdempotencyRecord{}
	for k, v := range u.base.idempotency.records {
		idem[k] = v
	}
	err := u.base.Do(ctx, func(r ports.Repositories) error {
		r.Reward = u.rewards
		r.Settlement = &memorySettlementStore{outboxes: u.rewards.outboxes}
		return fn(r)
	})
	if err != nil {
		u.rewards.grants = grants
		u.rewards.outboxes = boxes
		u.rewards.budgets = budgets
		u.base.idempotency.records = idem
	}
	return err
}
func flashFixture() (*CommunityTicketService, flashUnit) {
	b := newPeriodCreateUnit()
	u := flashUnit{b, &flashMemoryRewards{memoryRewardStore: newMemoryRewardStore(), setup: b.reward}}
	s := NewCommunityTicketService(u)
	s.QuotaPerUnit = 500000
	s.CheckinFlashEnabled = true
	s.now = func() time.Time { return time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC) }
	return s, u
}
func TestCheckinFlashFixedAmountReplayAndIsolation(t *testing.T) {
	s, u := flashFixture()
	task := CheckinFlash{Day: "2026-11-18", Cents: 30, EventID: 42}
	first, err := s.GrantCheckinFlash(context.Background(), 9, task)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		got, err := s.GrantCheckinFlash(context.Background(), 9, task)
		if err != nil || got != first {
			t.Fatal(got, err)
		}
	}
	if len(u.rewards.grants) != 1 || len(u.rewards.outboxes) != 1 || u.rewards.grants[0].Amount != 150000 || len(u.base.audit.logs) != 1 {
		t.Fatal("duplicate or wrong payout")
	}
	if len(u.base.reward.definitions) != 150 || u.base.admin.periods[0].Status != "draft" {
		t.Fatal("ordinary draw rules altered")
	}
	changed := task
	changed.Cents = 31
	if _, err = s.GrantCheckinFlash(context.Background(), 9, changed); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("changed amount accepted", err)
	}
	changed = task
	changed.EventID = 43
	if _, err = s.GrantCheckinFlash(context.Background(), 9, changed); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("changed source accepted", err)
	}
	s.CheckinFlashEnabled = false
	s.QuotaPerUnit = 100000
	got, err := s.GrantCheckinFlash(context.Background(), 9, task)
	if err != nil || got != first || u.rewards.grants[0].Amount != 150000 {
		t.Fatal("replay repriced", got, err)
	}
	status, err := s.CheckinFlashStatus(context.Background(), 9, first.GrantID)
	if err != nil || status.Status != "pending" {
		t.Fatal(status, err)
	}
	u.rewards.outboxes[0].Status = OutboxStatusDead
	status, err = s.CheckinFlashStatus(context.Background(), 9, first.GrantID)
	if err != nil || status.Status != "blocked" {
		t.Fatal(status, err)
	}
	u.rewards.grants[0].Status = "settled"
	status, err = s.CheckinFlashStatus(context.Background(), 9, first.GrantID)
	if err != nil || status.Status != "settled" {
		t.Fatal(status, err)
	}
	if _, err = s.CheckinFlashStatus(context.Background(), 10, first.GrantID); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("cross-user receipt read", err)
	}
}
func TestCheckinFlashRollbackAndExactConversion(t *testing.T) {
	s, u := flashFixture()
	task := CheckinFlash{Day: "2026-11-18", Cents: 150, EventID: 42}
	u.rewards.failOutbox = true
	if _, err := s.GrantCheckinFlash(context.Background(), 9, task); err == nil {
		t.Fatal("failure ignored")
	}
	if len(u.rewards.grants) != 0 || len(u.base.admin.periods) != 0 || len(u.base.audit.logs) != 0 {
		t.Fatal("partial issuance")
	}
	u.rewards.failOutbox = false
	if _, err := s.GrantCheckinFlash(context.Background(), 9, task); err != nil {
		t.Fatal(err)
	}
	if u.rewards.grants[0].Amount != 750000 {
		t.Fatal("wrong maximum")
	}
	for _, cents := range []int64{0, 151, -1} {
		bad := task
		bad.Cents = cents
		if _, err := s.GrantCheckinFlash(context.Background(), 9, bad); err == nil {
			t.Fatal("out of range")
		}
	}
	s2, _ := flashFixture()
	s2.QuotaPerUnit = 3
	if _, err := s2.GrantCheckinFlash(context.Background(), 9, task); err == nil {
		t.Fatal("rounded fractional quota")
	}
	s2.CheckinFlashEnabled = false
	if _, err := s2.GrantCheckinFlash(context.Background(), 9, task); err == nil {
		t.Fatal("paused issue")
	}
}

func TestCheckinFlashUsesExistingSettlementAndLostResponseRecovery(t *testing.T) {
	for _, lost := range []bool{false, true} {
		s, u := flashFixture()
		result, err := s.GrantCheckinFlash(context.Background(), 9, CheckinFlash{Day: "2026-11-18", Cents: 1, EventID: 42})
		if err != nil {
			t.Fatal(err)
		}
		boxes := &memorySettlementStore{outboxes: u.rewards.outboxes}
		client := &fakeBenefitClient{grantResponse: ports.BenefitGrantResponse{Applied: true, SourceRef: result.GrantID}}
		if lost {
			client.grantErr = errors.New("response lost")
			client.queryState = ports.BenefitState{Applied: true, SourceRef: result.GrantID}
		}
		worker, err := NewSettlementService(settlementUnit{store: newMemoryLedgerStore(), reward: u.rewards.memoryRewardStore, settlement: boxes}, client, SettlementConfig{BatchSize: 10, Now: s.now})
		if err != nil {
			t.Fatal(err)
		}
		report, err := worker.ProcessBatch(context.Background())
		if err != nil || report.Completed != 1 {
			t.Fatal(report, err)
		}
		if client.lastGrant.Amount != 5000 || client.lastGrant.SourceRef != result.GrantID || u.rewards.grants[0].Status != "settled" {
			t.Fatal(client.lastGrant, u.rewards.grants)
		}
		if lost && client.queryRef != result.GrantID {
			t.Fatal("changed source on retry")
		}
		report, err = worker.ProcessBatch(context.Background())
		if err != nil || report.Claimed != 0 || client.grantCalls != 1 {
			t.Fatal("duplicate credit", report, err)
		}
	}
}

func TestCheckinSnapshotKeyCannotBePublishedAsOrdinaryDrawRule(t *testing.T) {
	cmd := validCreateCommand()
	cmd.Key = checkinFlashPrefix + "500000"
	if _, err := normalizePeriodCreateCommand(cmd); err == nil {
		t.Fatal("reserved internal key accepted")
	}
}
