package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type memoryUnit struct{ store *memoryLedgerStore }

func (u memoryUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Ledger: u.store, Account: u.store})
}

type memoryLedgerStore struct {
	entries  []ledger.Entry
	accounts map[string]ledger.Account
	nextID   uint64
}

func newMemoryLedgerStore() *memoryLedgerStore {
	return &memoryLedgerStore{accounts: make(map[string]ledger.Account), nextID: 1}
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
