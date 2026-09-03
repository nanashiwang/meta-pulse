package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type memoryAuditStore struct {
	logs []ports.AuditLog
}

func (m *memoryAuditStore) Append(_ context.Context, log ports.AuditLog) error {
	m.logs = append(m.logs, log)
	return nil
}

type adminAdjustmentUnit struct {
	ledger *memoryLedgerStore
	idem   *memoryIdempotencyStore
	audit  *memoryAuditStore
}

func (u adminAdjustmentUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Ledger: u.ledger, Account: u.ledger, Idempotency: u.idem, Audit: u.audit})
}

func TestAdminAdjustmentUsesAppendOnlyLedgerAndAudit(t *testing.T) {
	store := newMemoryLedgerStore()
	idempotency := newMemoryIdempotencyStore()
	audit := &memoryAuditStore{}
	s, err := NewAdminAdjustmentService(adminAdjustmentUnit{ledger: store, idem: idempotency, audit: audit}, func() time.Time { return time.Unix(1700000000, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	command := AdminAdjustmentCommand{ActorType: "admin", ActorID: "operator-1", UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Amount: 250, Reason: "补发漏记用量", RequestID: "adjust-1"}
	first, err := s.Apply(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Apply(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if first.Entry.ID != second.Entry.ID || len(store.entries) != 1 || len(audit.logs) != 1 {
		t.Fatalf("first=%+v second=%+v entries=%d audits=%d", first, second, len(store.entries), len(audit.logs))
	}
	if store.entries[0].Operation != ledger.OperationContributionAdjustment || store.entries[0].Reason != command.Reason {
		t.Fatalf("entry=%+v", store.entries[0])
	}
	if audit.logs[0].ActorID != "operator-1" || audit.logs[0].RequestID != "adjust-1" {
		t.Fatalf("audit=%+v", audit.logs[0])
	}
}

func TestAdminAdjustmentSameRequestDifferentPayloadConflicts(t *testing.T) {
	store := newMemoryLedgerStore()
	s, err := NewAdminAdjustmentService(adminAdjustmentUnit{ledger: store, idem: newMemoryIdempotencyStore(), audit: &memoryAuditStore{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := AdminAdjustmentCommand{ActorType: "admin", ActorID: "operator-1", UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Amount: 2, Reason: "修复", RequestID: "adjust-1"}
	if _, err := s.Apply(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	base.Amount = 3
	if _, err := s.Apply(context.Background(), base); !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("error=%v, want idempotency conflict", err)
	}
}

func TestAdminTicketDebitCannotMakeBalanceNegative(t *testing.T) {
	store := newMemoryLedgerStore()
	s, err := NewAdminAdjustmentService(adminAdjustmentUnit{ledger: store, idem: newMemoryIdempotencyStore(), audit: &memoryAuditStore{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(context.Background(), AdminAdjustmentCommand{ActorType: "admin", ActorID: "operator-1", UserID: 9, PeriodID: 4, AssetType: ledger.AssetTicket, Amount: -1, Reason: "撤销", RequestID: "adjust-1"})
	if !errors.Is(err, ErrInsufficientTickets) {
		t.Fatalf("error=%v, want insufficient tickets", err)
	}
	if len(store.entries) != 0 {
		t.Fatalf("entries=%d, want 0", len(store.entries))
	}
}
