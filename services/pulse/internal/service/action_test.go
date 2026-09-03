package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type actionUnit struct {
	store  *memoryLedgerStore
	reward *memoryRewardStore
	idem   *memoryIdempotencyStore
}

func (u actionUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{
		Ledger: u.store, Account: u.store, Usage: memoryUsageRepo{u.store}, Conflict: memoryConflictRepo{u.store},
		Cursor: memoryCursorRepo{u.store}, Period: memoryPeriodRepo{u.store}, Economics: memoryEconomicsRepo{u.store},
		UserPeriod: memoryStatRepo{u.store}, Reward: u.reward, Idempotency: u.idem,
	})
}

type memoryRewardStore struct {
	definitions []reward.Definition
	budgets     map[string]ports.RewardBudget
	grants      []ports.RewardGrant
	outboxes    []ports.SettlementOutbox
}

func newMemoryRewardStore() *memoryRewardStore {
	return &memoryRewardStore{budgets: make(map[string]ports.RewardBudget)}
}

func (m *memoryRewardStore) ListDefinitions(context.Context, uint64) ([]reward.Definition, error) {
	return append([]reward.Definition(nil), m.definitions...), nil
}
func (m *memoryRewardStore) GetBudgetForUpdate(_ context.Context, periodID uint64, budgetType string) (ports.RewardBudget, error) {
	budget, ok := m.budgets[budgetKey(periodID, budgetType)]
	if !ok {
		return ports.RewardBudget{}, ports.ErrNotFound
	}
	return budget, nil
}
func (m *memoryRewardStore) SaveBudget(_ context.Context, budget ports.RewardBudget) error {
	m.budgets[budgetKey(budget.PeriodID, budget.BudgetType)] = budget
	return nil
}
func (m *memoryRewardStore) FindGrantByID(_ context.Context, grantID uint64) (*ports.RewardGrant, error) {
	for _, grant := range m.grants {
		if grant.ID == grantID {
			copy := grant
			return &copy, nil
		}
	}
	return nil, ports.ErrNotFound
}
func (m *memoryRewardStore) UpdateGrantStatus(_ context.Context, grantID uint64, status string, settledAt, reversedAt *time.Time) error {
	for i := range m.grants {
		if m.grants[i].ID == grantID {
			m.grants[i].Status, m.grants[i].SettledAt, m.grants[i].ReversedAt = status, settledAt, reversedAt
			return nil
		}
	}
	return ports.ErrNotFound
}

func (m *memoryRewardStore) FindGrantByAction(_ context.Context, periodID, userID uint64, actionID string) (*ports.RewardGrant, error) {
	for _, grant := range m.grants {
		if grant.PeriodID == periodID && grant.UserID == userID && grant.ActionID == actionID {
			copy := grant
			return &copy, nil
		}
	}
	return nil, ports.ErrNotFound
}
func (m *memoryRewardStore) CreateGrant(_ context.Context, grant ports.RewardGrant) (ports.RewardGrant, error) {
	if existing, err := m.FindGrantByAction(context.Background(), grant.PeriodID, grant.UserID, grant.ActionID); err == nil && existing != nil {
		return ports.RewardGrant{}, ports.ErrConflict
	}
	grant.ID = uint64(len(m.grants) + 1)
	m.grants = append(m.grants, grant)
	return grant, nil
}
func (m *memoryRewardStore) CreateOutbox(_ context.Context, outbox ports.SettlementOutbox) (ports.SettlementOutbox, error) {
	outbox.ID = uint64(len(m.outboxes) + 1)
	m.outboxes = append(m.outboxes, outbox)
	return outbox, nil
}

func budgetKey(periodID uint64, budgetType string) string {
	return fmt.Sprintf("%d:%s", periodID, budgetType)
}

type memoryIdempotencyStore struct {
	records map[string]ports.IdempotencyRecord
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{records: make(map[string]ports.IdempotencyRecord)}
}
func (m *memoryIdempotencyStore) GetOrCreateForUpdate(_ context.Context, scope, key, payloadHash string) (ports.IdempotencyRecord, error) {
	mapKey := scope + ":" + key
	if record, ok := m.records[mapKey]; ok {
		return record, nil
	}
	record := ports.IdempotencyRecord{ID: uint64(len(m.records) + 1), Scope: scope, Key: key, PayloadHash: payloadHash}
	m.records[mapKey] = record
	return record, nil
}
func (m *memoryIdempotencyStore) Save(_ context.Context, record ports.IdempotencyRecord) error {
	m.records[record.Scope+":"+record.Key] = record
	return nil
}

func newActionService(t *testing.T, store *memoryLedgerStore, rewardStore *memoryRewardStore, idem *memoryIdempotencyStore) *ActionService {
	t.Helper()
	at := time.Unix(1_700_000_000, 0).UTC()
	service, err := NewActionService(actionUnit{store: store, reward: rewardStore, idem: idem}, ActionConfig{
		RandomSecret: []byte("action-test-secret"), ShadowMode: true, Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func setupActionStore() (*memoryLedgerStore, *memoryRewardStore, *memoryIdempotencyStore) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Key: "p4", Status: period.StatusActive, StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour), ConfigVersion: "v1"}}
	store.accounts[accountKey(9, 4, ledger.AssetTicket)] = ledger.Account{ID: 1, UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Balance: 1}
	rewardStore := newMemoryRewardStore()
	rewardStore.definitions = []reward.Definition{{ID: 2, RewardKey: "quota-10", RewardType: "quota", Amount: 10, Weight: 1, ConfigVersion: "v1", Enabled: true}}
	rewardStore.budgets[budgetKey(4, ActionBudgetType)] = ports.RewardBudget{ID: 3, PeriodID: 4, BudgetType: ActionBudgetType, HardCap: 100, Version: 0}
	return store, rewardStore, newMemoryIdempotencyStore()
}

func TestActionReplay100TimesHasOneTicketGrantAndRandom(t *testing.T) {
	store, rewardStore, idem := setupActionStore()
	action := newActionService(t, store, rewardStore, idem)
	command := ActionCommand{UserID: 9, ActionID: "action-1", TriggerType: "pulse", IdempotencyKey: "idem-1"}
	var first ActionResult
	for i := 0; i < 100; i++ {
		result, err := action.Execute(context.Background(), command)
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		if i == 0 {
			first = result
		} else if result != first {
			t.Fatalf("replay result changed: first=%+v result=%+v", first, result)
		}
	}
	if len(store.entries) != 1 || len(rewardStore.grants) != 1 || len(rewardStore.outboxes) != 1 || store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance != 0 {
		t.Fatalf("entries=%d grants=%d outboxes=%d tickets=%d", len(store.entries), len(rewardStore.grants), len(rewardStore.outboxes), store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance)
	}
	if got := rewardStore.budgets[budgetKey(4, ActionBudgetType)].ReservedAmount; got != 10 {
		t.Fatalf("reserved budget=%d", got)
	}
	if rewardStore.outboxes[0].Status != OutboxStatusShadow || first.TransferableQuota {
		t.Fatalf("shadow result=%+v outbox=%+v", first, rewardStore.outboxes[0])
	}
}

func TestActionSameIdempotencyKeyDifferentPayloadConflicts(t *testing.T) {
	store, rewardStore, idem := setupActionStore()
	action := newActionService(t, store, rewardStore, idem)
	if _, err := action.Execute(context.Background(), ActionCommand{UserID: 9, ActionID: "one", TriggerType: "pulse", IdempotencyKey: "same"}); err != nil {
		t.Fatal(err)
	}
	_, err := action.Execute(context.Background(), ActionCommand{UserID: 9, ActionID: "two", TriggerType: "pulse", IdempotencyKey: "same"})
	if err == nil || !strings.Contains(err.Error(), ledger.ErrIdempotencyConflict.Error()) {
		t.Fatalf("err=%v", err)
	}
	if len(rewardStore.grants) != 1 {
		t.Fatalf("grants=%d", len(rewardStore.grants))
	}
}

func TestActionDifferentIdempotencyKeySameActionDoesNotSpendAgain(t *testing.T) {
	store, rewardStore, idem := setupActionStore()
	action := newActionService(t, store, rewardStore, idem)
	first, err := action.Execute(context.Background(), ActionCommand{UserID: 9, ActionID: "one", TriggerType: "pulse", IdempotencyKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := action.Execute(context.Background(), ActionCommand{UserID: 9, ActionID: "one", TriggerType: "pulse", IdempotencyKey: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(store.entries) != 1 || len(rewardStore.grants) != 1 {
		t.Fatalf("first=%+v second=%+v entries=%d grants=%d", first, second, len(store.entries), len(rewardStore.grants))
	}
}

func TestActionRequiresTicketAndBudget(t *testing.T) {
	store, rewardStore, idem := setupActionStore()
	store.accounts[accountKey(9, 4, ledger.AssetTicket)] = ledger.Account{ID: 1, UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Balance: 0}
	action := newActionService(t, store, rewardStore, idem)
	_, err := action.Execute(context.Background(), ActionCommand{UserID: 9, ActionID: "no-ticket", TriggerType: "pulse", IdempotencyKey: "one"})
	if !errors.Is(err, ErrInsufficientTickets) || len(rewardStore.grants) != 0 || len(store.entries) != 0 {
		t.Fatalf("err=%v grants=%d entries=%d", err, len(rewardStore.grants), len(store.entries))
	}
}

func TestConcurrentActionsCannotSpendSameTicketOrBudget(t *testing.T) {
	store, rewardStore, idem := setupActionStore()
	base := actionUnit{store: store, reward: rewardStore, idem: idem}
	action, err := NewActionService(&serialActionUnit{base: base}, ActionConfig{
		RandomSecret: []byte("action-test-secret"), ShadowMode: true,
		Now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	command := ActionCommand{UserID: 9, ActionID: "concurrent", TriggerType: "pulse", IdempotencyKey: "concurrent"}
	results := make(chan ActionResult, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			result, runErr := action.Execute(context.Background(), command)
			results <- result
			errs <- runErr
		}()
	}
	var first ActionResult
	for i := 0; i < 16; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if i == 0 {
			first = result
		} else if result != first {
			t.Fatalf("result changed under concurrency: first=%+v result=%+v", first, result)
		}
	}
	if len(store.entries) != 1 || len(rewardStore.grants) != 1 || rewardStore.budgets[budgetKey(4, ActionBudgetType)].ReservedAmount != 10 {
		t.Fatalf("entries=%d grants=%d budget=%+v", len(store.entries), len(rewardStore.grants), rewardStore.budgets[budgetKey(4, ActionBudgetType)])
	}
}

type serialActionUnit struct {
	mu   sync.Mutex
	base actionUnit
}

func (u *serialActionUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.base.Do(ctx, fn)
}
