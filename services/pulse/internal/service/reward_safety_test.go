package service

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"testing"
	"time"
)

func TestPausedActionsReplayCommittedResultButRejectNew(t *testing.T) {
	store, rewards, idem := setupActionStore()
	s := newActionService(t, store, rewards, idem)
	command := ActionCommand{UserID: 9, ActionID: "original", TriggerType: "pulse", IdempotencyKey: "key"}
	first, err := s.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.DisableNewActions = true
	for i := 0; i < 100; i++ {
		got, err := s.Execute(context.Background(), command)
		if err != nil || got.GrantID != first.GrantID {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	}
	command.ActionID = "another"
	command.IdempotencyKey = "other"
	if _, err := s.Execute(context.Background(), command); !errors.Is(err, ErrActionsUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if len(rewards.grants) != 1 || len(rewards.outboxes) != 1 {
		t.Fatal("paused request changed rewards")
	}
}
func TestLegacyTicketsCannotEnterVerifiedRewardPeriod(t *testing.T) {
	store, rewards, idem := setupActionStore()
	s := newActionService(t, store, rewards, idem)
	s.cfg.RequireVerifiedFunding = true
	command := ActionCommand{UserID: 9, ActionID: "new", TriggerType: "pulse", IdempotencyKey: "new"}
	if _, err := s.Execute(context.Background(), command); !errors.Is(err, ErrActionsUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if len(rewards.grants) != 0 || len(store.entries) != 0 {
		t.Fatal("legacy tickets spent")
	}
	store.periods[0].FundingPolicy = period.VerifiedPaidFunding
	store.periods[0].TicketThresholdMilli = 1000
	if _, err := s.Execute(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}
func TestBudgetStopsWholePoolWithoutChangingProbabilities(t *testing.T) {
	store, rewards, idem := setupActionStore()
	s := newActionService(t, store, rewards, idem)
	rewards.definitions = append(rewards.definitions, reward.Definition{ID: 3, RewardKey: "large", RewardType: "quota", Amount: 101, Weight: 1, ConfigVersion: "v1", Enabled: true})
	for i := 0; i < 100; i++ {
		_, err := s.Execute(context.Background(), ActionCommand{UserID: 9, ActionID: "attempt", TriggerType: "pulse", IdempotencyKey: "attempt"})
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("err=%v", err)
		}
	}
	if len(store.entries) != 0 || len(rewards.grants) != 0 {
		t.Fatal("exhausted pool consumed a ticket")
	}
}
func TestFundingProofCannotMintTwiceUnderDifferentLogIDs(t *testing.T) {
	store, rewards, idem := setupActionStore()
	store.entries = nil
	store.periods[0].StartsAt = time.Unix(1600000000, 0)
	store.periods[0].EndsAt = time.Unix(1800000000, 0)
	store.rules[4] = []economics.Rule{{ID: 8, Key: "paid", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	ingest, err := NewUsageIngestService(actionUnit{store: store, reward: rewards, idem: idem}, staticUsageSource{}, UsageIngestConfig{BatchSize: 100, TicketThresholdMilli: 1000})
	if err != nil {
		t.Fatal(err)
	}
	first := ingestEvent("1", "hash-1", usage.EventConsume, 1000)
	first.FundingProof = "wallet:one"
	var result IngestResult
	if err := ingest.processOne(context.Background(), first, &result); err != nil {
		t.Fatal(err)
	}
	entries := len(store.entries)
	for i := 0; i < 100; i++ {
		if err := ingest.processOne(context.Background(), first, &result); err != nil {
			t.Fatal(err)
		}
	}
	duplicate := first
	duplicate.SourceEventID = "2"
	duplicate.CursorValue = "1:2"
	duplicate.PayloadHash = "hash-2"
	if err := ingest.processOne(context.Background(), duplicate, &result); err != nil {
		t.Fatal(err)
	}
	if len(store.entries) != entries || result.ManualReview != 1 || result.Accepted != 1 {
		t.Fatalf("result=%+v entries=%d", result, len(store.entries))
	}
}
