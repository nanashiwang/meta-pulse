package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type settlementUnit struct {
	store      *memoryLedgerStore
	reward     *memoryRewardStore
	settlement *memorySettlementStore
}

func (u settlementUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Ledger: u.store, Account: u.store, Usage: memoryUsageRepo{u.store}, Conflict: memoryConflictRepo{u.store}, Cursor: memoryCursorRepo{u.store}, Period: memoryPeriodRepo{u.store}, Economics: memoryEconomicsRepo{u.store}, UserPeriod: memoryStatRepo{u.store}, Reward: u.reward, Settlement: u.settlement})
}

type memorySettlementStore struct{ outboxes []ports.SettlementOutbox }

func (m *memorySettlementStore) ClaimDue(_ context.Context, now time.Time, limit int, leaseUntil time.Time) ([]ports.SettlementOutbox, error) {
	result := make([]ports.SettlementOutbox, 0, limit)
	for i := range m.outboxes {
		outbox := &m.outboxes[i]
		if len(result) >= limit || (outbox.Status != OutboxStatusPending && outbox.Status != OutboxStatusRetry) || outbox.NextAttemptAt.After(now) {
			continue
		}
		outbox.Status = OutboxStatusProcessing
		outbox.LeasedUntil = &leaseUntil
		outbox.Attempts++
		result = append(result, *outbox)
	}
	return result, nil
}
func (m *memorySettlementStore) ListForReconciliation(_ context.Context, limit int) ([]ports.SettlementOutbox, error) {
	result := make([]ports.SettlementOutbox, 0, limit)
	for _, outbox := range m.outboxes {
		if outbox.Status == OutboxStatusProcessing || outbox.Status == OutboxStatusRetry || outbox.Status == OutboxStatusDead {
			result = append(result, outbox)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (m *memorySettlementStore) SaveOutbox(_ context.Context, outbox ports.SettlementOutbox) error {
	for i := range m.outboxes {
		if m.outboxes[i].ID == outbox.ID {
			m.outboxes[i] = outbox
			return nil
		}
	}
	return ports.ErrNotFound
}

type fakeBenefitClient struct {
	grantCalls    int
	queryCalls    int
	rollbackCalls int
	grantResponse ports.BenefitGrantResponse
	grantErr      error
	queryState    ports.BenefitState
	queryErr      error
	rollbackState ports.BenefitState
	rollbackErr   error
	lastGrant     ports.BenefitGrantRequest
	queryRef      string
	rollbackRef   string
}

func (f *fakeBenefitClient) Grant(_ context.Context, request ports.BenefitGrantRequest) (ports.BenefitGrantResponse, error) {
	f.grantCalls++
	f.lastGrant = request
	return f.grantResponse, f.grantErr
}
func (f *fakeBenefitClient) Query(_ context.Context, sourceRef string) (ports.BenefitState, error) {
	f.queryCalls++
	f.queryRef = sourceRef
	return f.queryState, f.queryErr
}
func (f *fakeBenefitClient) Rollback(_ context.Context, sourceRef, _ string) (ports.BenefitState, error) {
	f.rollbackCalls++
	f.rollbackRef = sourceRef
	return f.rollbackState, f.rollbackErr
}

func settlementFixture(t *testing.T, client *fakeBenefitClient) (*SettlementService, *memoryRewardStore, *memorySettlementStore, *ports.RewardGrant) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	pulseStore := newMemoryLedgerStore()
	pulseStore.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)}}
	rewardStore := newMemoryRewardStore()
	rewardStore.budgets[budgetKey(4, ActionBudgetType)] = ports.RewardBudget{ID: 2, PeriodID: 4, BudgetType: ActionBudgetType, HardCap: 100, ReservedAmount: 10, Version: 1}
	grant := ports.RewardGrant{ID: 1, GrantID: "pg_test", PeriodID: 4, UserID: 9, ActionID: "action", RewardType: "quota", Amount: 10, BudgetType: ActionBudgetType, RandomValue: "random", ConfigVersion: "v1", Status: RewardStatusPending, SourceRef: "pg_test", Reason: "shadow"}
	rewardStore.grants = append(rewardStore.grants, grant)
	payload, _ := json.Marshal(settlementPayload{GrantID: grant.GrantID, UserID: grant.UserID, Amount: grant.Amount, SourceRef: grant.SourceRef, RewardType: grant.RewardType})
	outboxStore := &memorySettlementStore{outboxes: []ports.SettlementOutbox{{ID: 1, RewardGrantID: grant.ID, Operation: "grant", PayloadHash: sha256Hex(payload), PayloadJSON: payload, Status: OutboxStatusPending, NextAttemptAt: now.Add(-time.Second)}}}
	service, err := NewSettlementService(settlementUnit{store: pulseStore, reward: rewardStore, settlement: outboxStore}, client, SettlementConfig{BatchSize: 10, Lease: time.Minute, BaseBackoff: time.Second, MaxBackoff: time.Minute, MaxAttempts: 3, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, rewardStore, outboxStore, &rewardStore.grants[0]
}

func TestSettlementSuccessMovesReservedBudgetAndClosesOutbox(t *testing.T) {
	client := &fakeBenefitClient{grantResponse: ports.BenefitGrantResponse{Applied: true, SourceRef: "pg_test"}}
	service, rewards, outboxes, _ := settlementFixture(t, client)
	report, err := service.ProcessBatch(context.Background())
	if err != nil || report.Completed != 1 || client.grantCalls != 1 || client.queryCalls != 0 {
		t.Fatalf("report=%+v err=%v client=%+v", report, err, client)
	}
	if outboxes.outboxes[0].Status != OutboxStatusCompleted || rewards.grants[0].Status != GrantStatusSettled {
		t.Fatalf("outbox=%+v grant=%+v", outboxes.outboxes[0], rewards.grants[0])
	}
	budget := rewards.budgets[budgetKey(4, ActionBudgetType)]
	if budget.ReservedAmount != 0 || budget.SettledAmount != 10 {
		t.Fatalf("budget=%+v", budget)
	}
	if client.lastGrant.TransferableQuota || client.lastGrant.GrantID != "pg_test" || client.lastGrant.SourceRef != "pg_test" || client.lastGrant.PayloadHash != outboxes.outboxes[0].PayloadHash {
		t.Fatalf("benefit request=%+v", client.lastGrant)
	}
}

func TestSettlementTimeoutQueriesOriginalSourceRef(t *testing.T) {
	client := &fakeBenefitClient{grantErr: errors.New("context deadline exceeded"), queryState: ports.BenefitState{Applied: true, SourceRef: "pg_test"}}
	service, _, outboxes, _ := settlementFixture(t, client)
	report, err := service.ProcessBatch(context.Background())
	if err != nil || report.Completed != 1 || client.grantCalls != 1 || client.queryCalls != 1 || client.queryRef != "pg_test" {
		t.Fatalf("report=%+v err=%v client=%+v", report, err, client)
	}
	if outboxes.outboxes[0].Status != OutboxStatusCompleted {
		t.Fatalf("outbox=%+v", outboxes.outboxes[0])
	}
}

func TestSettlementConflictBecomesDeadWithoutChangingSource(t *testing.T) {
	client := &fakeBenefitClient{grantErr: ports.ErrBenefitPayloadConflict, queryState: ports.BenefitState{Applied: false, SourceRef: "pg_test"}}
	service, _, outboxes, _ := settlementFixture(t, client)
	report, err := service.ProcessBatch(context.Background())
	if err != nil || report.Dead != 1 || outboxes.outboxes[0].Status != OutboxStatusDead {
		t.Fatalf("report=%+v err=%v outbox=%+v", report, err, outboxes.outboxes[0])
	}
	if client.queryRef != "pg_test" {
		t.Fatalf("query source ref=%q", client.queryRef)
	}
}

func TestSettlementRollbackReleasesBudgetAndMarksGrant(t *testing.T) {
	client := &fakeBenefitClient{rollbackState: ports.BenefitState{Applied: true, SourceRef: "pg_test"}}
	service, rewards, _, grant := settlementFixture(t, client)
	rewards.grants[0].Status = GrantStatusSettled
	rewards.budgets[budgetKey(4, ActionBudgetType)] = ports.RewardBudget{ID: 2, PeriodID: 4, BudgetType: ActionBudgetType, HardCap: 100, SettledAmount: 10, Version: 1}
	if err := service.Rollback(context.Background(), grant.ID, "fraud review"); err != nil {
		t.Fatal(err)
	}
	if client.rollbackCalls != 1 || client.rollbackRef != "pg_test" || rewards.grants[0].Status != GrantStatusReversed {
		t.Fatalf("client=%+v grant=%+v", client, rewards.grants[0])
	}
	budget := rewards.budgets[budgetKey(4, ActionBudgetType)]
	if budget.SettledAmount != 0 || budget.ReleasedAmount != 10 {
		t.Fatalf("budget=%+v", budget)
	}
	// A retry after the local commit must be a no-op. In particular it must
	// not call new-api again or release the same budget amount twice.
	if err := service.Rollback(context.Background(), grant.ID, "fraud review retry"); err != nil {
		t.Fatal(err)
	}
	if client.rollbackCalls != 1 || rewards.budgets[budgetKey(4, ActionBudgetType)].ReleasedAmount != 10 {
		t.Fatalf("rollback was not idempotent: client=%+v budget=%+v", client, rewards.budgets[budgetKey(4, ActionBudgetType)])
	}
}

func TestSettlementRejectsTamperedPayload(t *testing.T) {
	client := &fakeBenefitClient{}
	service, _, outboxes, _ := settlementFixture(t, client)
	outboxes.outboxes[0].PayloadJSON = []byte(`{"user_id":9,"amount":999,"source_ref":"pg_test","reward_type":"quota"}`)
	report, err := service.ProcessBatch(context.Background())
	if err != nil || report.Dead != 1 || client.grantCalls != 0 {
		t.Fatalf("report=%+v err=%v client=%+v", report, err, client)
	}
}

func TestSettlementRejectsMismatchedGrantIDWithValidPayloadHash(t *testing.T) {
	client := &fakeBenefitClient{}
	service, _, outboxes, _ := settlementFixture(t, client)
	payload, err := json.Marshal(settlementPayload{GrantID: "other-grant", UserID: 9, Amount: 10, SourceRef: "pg_test", RewardType: "quota"})
	if err != nil {
		t.Fatal(err)
	}
	outboxes.outboxes[0].PayloadJSON = payload
	outboxes.outboxes[0].PayloadHash = sha256Hex(payload)
	report, err := service.ProcessBatch(context.Background())
	if err != nil || report.Dead != 1 || client.grantCalls != 0 {
		t.Fatalf("report=%+v err=%v client=%+v", report, err, client)
	}
}

func TestCanonicalJSONHashIgnoresMySQLFormatting(t *testing.T) {
	compact := []byte(`{"grant_id":"g1","user_id":9007199254740993,"amount":25,"transferable_quota":false}`)
	normalized := []byte(`{ "amount": 25, "transferable_quota": false, "user_id": 9007199254740993, "grant_id": "g1" }`)
	if canonicalJSONHash(compact) != canonicalJSONHash(normalized) {
		t.Fatalf("canonical JSON hash changed with key order or whitespace")
	}
}
