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
