package mysql

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func TestRewardGrantTransitionRejectsInvalidStateChanges(t *testing.T) {
	repo := &rewardRepository{}
	now := time.Now()
	cases := []struct {
		grantID uint64
		from    string
		to      string
		at      time.Time
	}{
		{0, "pending", "settled", now},
		{1, "pending", "settled", time.Time{}},
		{1, "pending", "reversed", now},
		{1, "settled", "pending", now},
		{1, "reversed", "settled", now},
	}
	for _, test := range cases {
		if err := repo.TransitionGrantStatus(context.Background(), test.grantID, test.from, test.to, test.at); err == nil {
			t.Fatalf("transition id=%d %s -> %s at=%s was accepted", test.grantID, test.from, test.to, test.at)
		}
	}
}

func TestSettlementClaimRejectsInvalidLeaseAndAttemptOverflow(t *testing.T) {
	repo := &rewardRepository{}
	now := time.Now()
	for _, leaseUntil := range []time.Time{time.Time{}, now, now.Add(-time.Second)} {
		if _, err := repo.ClaimDue(context.Background(), now, 1, leaseUntil); err == nil {
			t.Fatalf("lease ending at %s was accepted", leaseUntil)
		}
	}
	if _, err := repo.ClaimDue(context.Background(), time.Time{}, 1, now); err == nil {
		t.Fatal("zero claim time was accepted")
	}
	if next, err := nextSettlementAttempt(math.MaxUint32); err == nil || next != 0 {
		t.Fatalf("overflow attempt next=%d error=%v", next, err)
	}
	if next, err := nextSettlementAttempt(41); err != nil || next != 42 {
		t.Fatalf("normal attempt next=%d error=%v", next, err)
	}
}

func TestSettlementOutboxCreateRequiresCanonicalImmutablePayload(t *testing.T) {
	now := time.Now()
	payload := []byte(`{"grant_id":"g1","user_id":9007199254740993,"amount":10}`)
	hash, err := canonicalSettlementPayloadHash(payload)
	if err != nil {
		t.Fatal(err)
	}
	base := ports.SettlementOutbox{
		RewardGrantID: 1, Operation: "grant", PayloadHash: hash, PayloadJSON: payload,
		Status: "pending", NextAttemptAt: now, CreatedAt: now,
	}
	if err := validateSettlementOutboxCreate(base); err != nil {
		t.Fatalf("valid outbox rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ports.SettlementOutbox)
	}{
		{"explicit id", func(v *ports.SettlementOutbox) { v.ID = 1 }},
		{"missing grant", func(v *ports.SettlementOutbox) { v.RewardGrantID = 0 }},
		{"wrong operation", func(v *ports.SettlementOutbox) { v.Operation = "rollback" }},
		{"terminal status", func(v *ports.SettlementOutbox) { v.Status = "conflict" }},
		{"preclaimed", func(v *ports.SettlementOutbox) { v.Attempts = 1 }},
		{"zero schedule", func(v *ports.SettlementOutbox) { v.NextAttemptAt = time.Time{} }},
		{"zero creation", func(v *ports.SettlementOutbox) { v.CreatedAt = time.Time{} }},
		{"hash mismatch", func(v *ports.SettlementOutbox) { v.PayloadHash = strings.Repeat("0", 64) }},
		{"trailing json", func(v *ports.SettlementOutbox) { v.PayloadJSON = append(v.PayloadJSON, []byte(`{}`)...) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.PayloadJSON = append([]byte(nil), base.PayloadJSON...)
			test.mutate(&value)
			if err := validateSettlementOutboxCreate(value); err == nil {
				t.Fatal("invalid outbox create was accepted")
			}
		})
	}

	reordered := []byte(` { "amount": 10, "user_id": 9007199254740993, "grant_id": "g1" } `)
	reorderedHash, err := canonicalSettlementPayloadHash(reordered)
	if err != nil || reorderedHash != hash {
		t.Fatalf("canonical hash changed: first=%s second=%s error=%v", hash, reorderedHash, err)
	}
}

func TestSettlementOutboxUpdateEnforcesTerminalShape(t *testing.T) {
	now := time.Now()
	base := ports.SettlementOutbox{ID: 1, RewardGrantID: 2, Attempts: 1, NextAttemptAt: now, Status: "retry", LastError: "temporary"}
	if err := validateSettlementOutboxUpdate(base); err != nil {
		t.Fatalf("valid retry rejected: %v", err)
	}
	completed := base
	completed.Status = "completed"
	completed.LastError = ""
	completed.CompletedAt = &now
	if err := validateSettlementOutboxUpdate(completed); err != nil {
		t.Fatalf("valid completion rejected: %v", err)
	}
	cases := []ports.SettlementOutbox{
		{ID: 1, RewardGrantID: 2, Attempts: 0, NextAttemptAt: now, Status: "retry", LastError: "temporary"},
		{ID: 1, RewardGrantID: 2, Attempts: 1, NextAttemptAt: now, Status: "processing"},
		{ID: 1, RewardGrantID: 2, Attempts: 1, NextAttemptAt: now, Status: "dead"},
		{ID: 1, RewardGrantID: 2, Attempts: 1, NextAttemptAt: now, Status: "completed"},
	}
	for _, value := range cases {
		if err := validateSettlementOutboxUpdate(value); err == nil {
			t.Fatalf("invalid outbox update was accepted: %+v", value)
		}
	}
}

func TestRewardBudgetSaveRejectsBrokenInvariant(t *testing.T) {
	valid := ports.RewardBudget{ID: 1, PeriodID: 2, BudgetType: "instant", HardCap: 100, ReservedAmount: 30, SettledAmount: 60, ReleasedAmount: 10, Version: 3}
	if err := validateRewardBudgetSave(valid); err != nil {
		t.Fatalf("valid budget rejected: %v", err)
	}
	cases := []ports.RewardBudget{
		{ID: 0, PeriodID: 2, BudgetType: "instant", HardCap: 100, Version: 1},
		{ID: 1, PeriodID: 0, BudgetType: "instant", HardCap: 100, Version: 1},
		{ID: 1, PeriodID: 2, BudgetType: " instant", HardCap: 100, Version: 1},
		{ID: 1, PeriodID: 2, BudgetType: "instant", HardCap: 100, ReservedAmount: 60, SettledAmount: 50, Version: 1},
		{ID: 1, PeriodID: 2, BudgetType: "instant", HardCap: -1, Version: 1},
		{ID: 1, PeriodID: 2, BudgetType: "instant", HardCap: 100, ReleasedAmount: -1, Version: 1},
	}
	for _, budget := range cases {
		if err := validateRewardBudgetSave(budget); err == nil {
			t.Fatalf("invalid budget accepted: %+v", budget)
		}
	}
}

func TestRewardGrantCreateValidatesImmutableIdentity(t *testing.T) {
	now := time.Now()
	valid := ports.RewardGrant{
		GrantID: "pg_test", PeriodID: 2, UserID: 3, ActionID: "action", TriggerType: "pulse",
		RewardDefinitionID: 4, RewardType: "quota", Amount: 10, BudgetType: "instant",
		RandomValue: strings.Repeat("a", 64), ConfigVersion: "v1", Status: "pending",
		SourceRef: "pg_test", Reason: "pulse action", CreatedAt: now,
	}
	if err := validateRewardGrantCreate(valid); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	content := valid
	content.TriggerType = "content"
	content.RewardDefinitionID = 0
	if err := validateRewardGrantCreate(content); err != nil {
		t.Fatalf("valid content grant rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ports.RewardGrant)
	}{
		{"explicit id", func(v *ports.RewardGrant) { v.ID = 1 }},
		{"source mismatch", func(v *ports.RewardGrant) { v.SourceRef = "other" }},
		{"transferable", func(v *ports.RewardGrant) { v.TransferableQuota = true }},
		{"terminal status", func(v *ports.RewardGrant) { v.Status = "settled" }},
		{"invalid random", func(v *ports.RewardGrant) { v.RandomValue = strings.Repeat("z", 64) }},
		{"long reason", func(v *ports.RewardGrant) { v.Reason = strings.Repeat("理", 256) }},
		{"missing definition", func(v *ports.RewardGrant) { v.RewardDefinitionID = 0 }},
		{"content definition", func(v *ports.RewardGrant) { v.TriggerType = "content" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateRewardGrantCreate(value); err == nil {
				t.Fatal("invalid grant was accepted")
			}
		})
	}
}

func TestRewardRepositoryRejectsInvalidLookupIdentityBeforeQuery(t *testing.T) {
	repo := &rewardRepository{}
	if _, err := repo.ListDefinitions(context.Background(), 0); err == nil {
		t.Fatal("zero definition period was accepted")
	}
	if _, err := repo.GetBudgetForUpdate(context.Background(), 0, "instant"); err == nil {
		t.Fatal("zero budget period was accepted")
	}
	if _, err := repo.FindGrantByAction(context.Background(), 1, 0, "action"); err == nil {
		t.Fatal("zero grant user was accepted")
	}
	if _, err := repo.FindGrantByID(context.Background(), 0); err == nil {
		t.Fatal("zero grant id was accepted")
	}
	if _, err := repo.FindOutboxByGrant(context.Background(), 0); err == nil {
		t.Fatal("zero outbox grant id was accepted")
	}
}
