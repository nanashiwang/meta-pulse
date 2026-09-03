package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type memoryUnit struct{ store *memoryLedgerStore }

func (u memoryUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Ledger: u.store, Account: u.store, Usage: memoryUsageRepo{u.store}, Conflict: memoryConflictRepo{u.store}, Cursor: memoryCursorRepo{u.store}, Period: memoryPeriodRepo{u.store}, Economics: memoryEconomicsRepo{u.store}, UserPeriod: memoryStatRepo{u.store}})
}

type memoryLedgerStore struct {
	entries     []ledger.Entry
	accounts    map[string]ledger.Account
	nextID      uint64
	usageEvents []usage.Event
	conflicts   []ports.IngestConflict
	cursor      ports.Cursor
	periods     []period.Period
	rules       map[uint64][]economics.Rule
	stats       map[string]ports.UserPeriodStat
}

func newMemoryLedgerStore() *memoryLedgerStore {
	return &memoryLedgerStore{accounts: make(map[string]ledger.Account), nextID: 1, rules: make(map[uint64][]economics.Rule), stats: make(map[string]ports.UserPeriodStat)}
}

func idemKey(operation ledger.Operation, key string) string { return string(operation) + ":" + key }
func accountKey(userID, periodID uint64, asset ledger.AssetType) string {
	return fmt.Sprintf("%d:%d:%s", userID, periodID, asset)
}

func (m *memoryLedgerStore) FindByIdempotency(_ context.Context, operation ledger.Operation, key string) (*ledger.Entry, error) {
	for i := range m.entries {
		if idemKey(m.entries[i].Operation, m.entries[i].IdempotencyKey) == idemKey(operation, key) {
			entry := m.entries[i]
			return &entry, nil
		}
	}
	return nil, ports.ErrNotFound
}

func (m *memoryLedgerStore) FindBySource(_ context.Context, sourceType, sourceRef string, asset ledger.AssetType) (*ledger.Entry, error) {
	for i := range m.entries {
		if m.entries[i].SourceType == sourceType && m.entries[i].SourceRef == sourceRef && m.entries[i].AssetType == asset {
			entry := m.entries[i]
			return &entry, nil
		}
	}
	return nil, ports.ErrNotFound
}

func (m *memoryLedgerStore) Append(_ context.Context, entry ledger.Entry) (ledger.Entry, error) {
	if existing, _ := m.FindByIdempotency(context.Background(), entry.Operation, entry.IdempotencyKey); existing != nil {
		return ledger.Entry{}, ports.ErrConflict
	}
	entry.ID = m.nextID
	m.nextID++
	m.entries = append(m.entries, entry)
	return entry, nil
}

func (m *memoryLedgerStore) ListAccountEntries(_ context.Context, userID, periodID uint64, asset ledger.AssetType) ([]ledger.Entry, error) {
	var result []ledger.Entry
	for _, entry := range m.entries {
		if entry.UserID == userID && entry.PeriodID == periodID && entry.AssetType == asset {
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (m *memoryLedgerStore) GetOrCreateForUpdate(_ context.Context, userID, periodID uint64, asset ledger.AssetType) (ledger.Account, error) {
	key := accountKey(userID, periodID, asset)
	account, ok := m.accounts[key]
	if !ok {
		account = ledger.Account{ID: uint64(len(m.accounts) + 1), UserID: userID, PeriodID: periodID, AssetType: asset}
		m.accounts[key] = account
	}
	return account, nil
}

func (m *memoryLedgerStore) Save(_ context.Context, account ledger.Account) error {
	m.accounts[accountKey(account.UserID, account.PeriodID, account.AssetType)] = account
	return nil
}

func (m *memoryLedgerStore) ReplaceFromLedger(_ context.Context, _ uint64, rebuilt ledger.Account) error {
	m.accounts[accountKey(rebuilt.UserID, rebuilt.PeriodID, rebuilt.AssetType)] = rebuilt
	return nil
}

func (m *memoryLedgerStore) ListForUser(_ context.Context, userID uint64) ([]ledger.Account, error) {
	var result []ledger.Account
	for _, account := range m.accounts {
		if account.UserID == userID {
			result = append(result, account)
		}
	}
	return result, nil
}

func (m *memoryLedgerStore) ListAll(context.Context) ([]ledger.Account, error) {
	result := make([]ledger.Account, 0, len(m.accounts))
	for _, account := range m.accounts {
		result = append(result, account)
	}
	return result, nil
}

func testEntry(hash string) ledger.Entry {
	return ledger.Entry{UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Operation: ledger.OperationContributionEarn, Amount: 1000, SourceType: "usage", SourceRef: "newapi:1", IdempotencyKey: "newapi:1", PayloadHash: hash}
}

func TestLedgerAppendReplay100TimesHasOneEffect(t *testing.T) {
	store := newMemoryLedgerStore()
	service, err := NewLedgerService(memoryUnit{store: store})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		result, err := service.Append(context.Background(), testEntry("same-hash"))
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if result.ID != 1 || result.BalanceAfter != 1000 {
			t.Fatalf("append %d result = %+v", i, result)
		}
	}
	if len(store.entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(store.entries))
	}
	account := store.accounts[accountKey(9, 4, ledger.AssetContribution)]
	if account.Balance != 1000 || account.Version != 1 {
		t.Fatalf("account = %+v", account)
	}
}

func TestLedgerAppendSameKeyDifferentPayloadConflicts(t *testing.T) {
	store := newMemoryLedgerStore()
	service, _ := NewLedgerService(memoryUnit{store: store})
	if _, err := service.Append(context.Background(), testEntry("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Append(context.Background(), testEntry("changed")); !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want idempotency conflict", err)
	}
}

func TestLedgerAccountCanBeRebuilt(t *testing.T) {
	store := newMemoryLedgerStore()
	service, _ := NewLedgerService(memoryUnit{store: store})
	if _, err := service.Append(context.Background(), testEntry("same")); err != nil {
		t.Fatal(err)
	}
	key := accountKey(9, 4, ledger.AssetContribution)
	corrupt := store.accounts[key]
	corrupt.Balance = 7
	corrupt.Version = 8
	store.accounts[key] = corrupt

	rebuilt, err := service.RebuildAccount(context.Background(), 9, 4, ledger.AssetContribution)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Balance != 1000 || rebuilt.Version != 1 || store.accounts[key] != rebuilt {
		t.Fatalf("rebuilt = %+v, stored = %+v", rebuilt, store.accounts[key])
	}
}

type memoryUsageRepo struct{ store *memoryLedgerStore }

func (r memoryUsageRepo) FindBySource(ctx context.Context, sourceSystem, sourceEventID string) (*usage.Event, error) {
	return r.store.FindUsageBySource(ctx, sourceSystem, sourceEventID)
}
func (r memoryUsageRepo) FindConsumeByRequest(ctx context.Context, userID uint64, requestID string) (*usage.Event, error) {
	return r.store.FindConsumeByRequest(ctx, userID, requestID)
}
func (r memoryUsageRepo) Create(ctx context.Context, event usage.Event) (usage.Event, error) {
	return r.store.Create(ctx, event)
}

type memoryConflictRepo struct{ store *memoryLedgerStore }

func (r memoryConflictRepo) Create(ctx context.Context, c ports.IngestConflict) error {
	return r.store.CreateConflict(ctx, c)
}

type memoryCursorRepo struct{ store *memoryLedgerStore }

func (r memoryCursorRepo) GetOrCreateForUpdate(ctx context.Context, n, s string) (ports.Cursor, error) {
	return r.store.GetCursor(ctx, n, s)
}
func (r memoryCursorRepo) Save(ctx context.Context, c ports.Cursor) error {
	return r.store.SaveCursor(ctx, c)
}

type memoryPeriodRepo struct{ store *memoryLedgerStore }

func (r memoryPeriodRepo) FindActiveAt(ctx context.Context, at time.Time) (period.Period, error) {
	return r.store.FindPeriod(ctx, at)
}

type memoryEconomicsRepo struct{ store *memoryLedgerStore }

func (r memoryEconomicsRepo) ListRules(ctx context.Context, id uint64) ([]economics.Rule, error) {
	return r.store.ListEconomics(ctx, id)
}

type memoryStatRepo struct{ store *memoryLedgerStore }

func (r memoryStatRepo) GetOrCreateForUpdate(ctx context.Context, u, p uint64) (ports.UserPeriodStat, error) {
	return r.store.GetStat(ctx, u, p)
}
func (r memoryStatRepo) Save(ctx context.Context, s ports.UserPeriodStat) error {
	return r.store.SaveStat(ctx, s)
}

// The in-memory repositories below are also used by Usage Ingest tests. They
// deliberately expose the same transaction-local ports as MySQL.

func (m *memoryLedgerStore) FindUsageBySource(_ context.Context, sourceSystem, sourceEventID string) (*usage.Event, error) {
	for i := range m.usageEvents {
		if m.usageEvents[i].SourceSystem == sourceSystem && m.usageEvents[i].SourceEventID == sourceEventID {
			event := m.usageEvents[i]
			return &event, nil
		}
	}
	return nil, ports.ErrNotFound
}

func (m *memoryLedgerStore) FindConsumeByRequest(_ context.Context, userID uint64, requestID string) (*usage.Event, error) {
	for i := len(m.usageEvents) - 1; i >= 0; i-- {
		if m.usageEvents[i].UserID == userID && m.usageEvents[i].RequestID == requestID && m.usageEvents[i].EventType == usage.EventConsume && m.usageEvents[i].Status == usage.StatusAccepted {
			event := m.usageEvents[i]
			return &event, nil
		}
	}
	return nil, ports.ErrNotFound
}

func (m *memoryLedgerStore) Create(_ context.Context, event usage.Event) (usage.Event, error) {
	if existing, _ := m.FindUsageBySource(context.Background(), event.SourceSystem, event.SourceEventID); existing != nil {
		return usage.Event{}, ports.ErrConflict
	}
	event.ID = uint64(len(m.usageEvents) + 1)
	m.usageEvents = append(m.usageEvents, event)
	return event, nil
}

func (m *memoryLedgerStore) CreateConflict(_ context.Context, conflict ports.IngestConflict) error {
	for _, existing := range m.conflicts {
		if existing.SourceSystem == conflict.SourceSystem && existing.SourceEventID == conflict.SourceEventID && existing.IncomingPayloadHash == conflict.IncomingPayloadHash {
			return nil
		}
	}
	m.conflicts = append(m.conflicts, conflict)
	return nil
}

func (m *memoryLedgerStore) GetCursor(_ context.Context, name, source string) (ports.Cursor, error) {
	if m.cursor.Name == "" {
		m.cursor = ports.Cursor{Name: name, SourceSystem: source}
	}
	return m.cursor, nil
}

func (m *memoryLedgerStore) SaveCursor(_ context.Context, cursor ports.Cursor) error {
	m.cursor = cursor
	return nil
}

func (m *memoryLedgerStore) FindPeriod(_ context.Context, at time.Time) (period.Period, error) {
	return period.ResolveActive(m.periods, at)
}

func (m *memoryLedgerStore) ListEconomics(_ context.Context, periodID uint64) ([]economics.Rule, error) {
	return m.rules[periodID], nil
}

func (m *memoryLedgerStore) GetStat(_ context.Context, userID, periodID uint64) (ports.UserPeriodStat, error) {
	key := fmt.Sprintf("%d:%d", userID, periodID)
	stat, ok := m.stats[key]
	if !ok {
		stat = ports.UserPeriodStat{ID: uint64(len(m.stats) + 1), UserID: userID, PeriodID: periodID}
		m.stats[key] = stat
	}
	return stat, nil
}

func (m *memoryLedgerStore) SaveStat(_ context.Context, stat ports.UserPeriodStat) error {
	m.stats[fmt.Sprintf("%d:%d", stat.UserID, stat.PeriodID)] = stat
	return nil
}
