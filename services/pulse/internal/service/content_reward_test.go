package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type memoryContentStore struct {
	candidates map[uint64]ports.ContentCandidate
	awards     map[string]ports.ContentAward
	nextID     uint64
}

func (m *memoryContentStore) FindCandidateBySource(_ context.Context, sourceSystem, sourceContentID string) (*ports.ContentCandidate, error) {
	for _, candidate := range m.candidates {
		if candidate.SourceSystem == sourceSystem && candidate.SourceContentID == sourceContentID {
			copy := candidate
			return &copy, nil
		}
	}
	return nil, ports.ErrNotFound
}
func (m *memoryContentStore) FindCandidateForUpdate(_ context.Context, candidateID uint64) (*ports.ContentCandidate, error) {
	candidate, ok := m.candidates[candidateID]
	if !ok {
		return nil, ports.ErrNotFound
	}
	copy := candidate
	return &copy, nil
}
func (m *memoryContentStore) CreateCandidate(_ context.Context, candidate ports.ContentCandidate) (ports.ContentCandidate, error) {
	if m.candidates == nil {
		m.candidates = make(map[uint64]ports.ContentCandidate)
	}
	if candidate.ID == 0 {
		m.nextID++
		candidate.ID = m.nextID
	}
	for _, existing := range m.candidates {
		if existing.SourceSystem == candidate.SourceSystem && existing.SourceContentID == candidate.SourceContentID {
			return ports.ContentCandidate{}, ports.ErrConflict
		}
	}
	m.candidates[candidate.ID] = candidate
	return candidate, nil
}
func (m *memoryContentStore) ReviewCandidate(_ context.Context, candidateID uint64, status, actorType, actorID, reason string, reviewedAt time.Time) error {
	candidate, ok := m.candidates[candidateID]
	if !ok {
		return ports.ErrNotFound
	}
	candidate.Status, candidate.ReviewActorType, candidate.ReviewActorID, candidate.ReviewReason = status, actorType, actorID, reason
	candidate.ReviewedAt = &reviewedAt
	m.candidates[candidateID] = candidate
	return nil
}
func (m *memoryContentStore) FindAwardByAction(_ context.Context, actionID string) (*ports.ContentAward, error) {
	award, ok := m.awards[actionID]
	if !ok {
		return nil, ports.ErrNotFound
	}
	copy := award
	return &copy, nil
}
func (m *memoryContentStore) CreateAward(_ context.Context, award ports.ContentAward) (ports.ContentAward, error) {
	if m.awards == nil {
		m.awards = make(map[string]ports.ContentAward)
	}
	if _, ok := m.awards[award.ActionID]; ok {
		return ports.ContentAward{}, ports.ErrConflict
	}
	award.ID = uint64(len(m.awards) + 1)
	m.awards[award.ActionID] = award
	return award, nil
}
func (m *memoryContentStore) UpdateAwardStatus(_ context.Context, actionID, status string) error {
	award, ok := m.awards[actionID]
	if !ok {
		return ports.ErrNotFound
	}
	award.Status = status
	m.awards[actionID] = award
	return nil
}
func (m *memoryContentStore) MarkAwardSettledByGrantID(_ context.Context, grantID string) error {
	for actionID, award := range m.awards {
		if award.GrantID == grantID && award.Status == ports.ContentAwardPending {
			award.Status = ports.ContentAwardSettled
			m.awards[actionID] = award
		}
	}
	return nil
}
func (m *memoryContentStore) LockAwardLimits(_ context.Context, userID, periodID uint64, day time.Time) error {
	if userID == 0 || periodID == 0 || day.IsZero() {
		return errors.New("invalid content award limit scope")
	}
	return nil
}
func (m *memoryContentStore) SumUserActiveAwards(_ context.Context, userID, periodID uint64) (int64, error) {
	var total int64
	for _, award := range m.awards {
		if award.UserID == userID && award.PeriodID == periodID && (award.Status == ports.ContentAwardPending || award.Status == ports.ContentAwardSettled) {
			total += award.Amount
		}
	}
	return total, nil
}
func (m *memoryContentStore) SumDailyActiveAwards(_ context.Context, _ time.Time) (int64, error) {
	var total int64
	for _, award := range m.awards {
		if award.Status == ports.ContentAwardPending || award.Status == ports.ContentAwardSettled {
			total += award.Amount
		}
	}
	return total, nil
}

type contentAwardUnit struct {
	ledger  *memoryLedgerStore
	reward  *memoryRewardStore
	idem    *memoryIdempotencyStore
	content *memoryContentStore
	audit   *memoryAuditStore
}

func (u contentAwardUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Ledger: u.ledger, Account: u.ledger, Reward: u.reward, Idempotency: u.idem, Content: u.content, Audit: u.audit})
}

func newContentAwardFixture(t *testing.T, contribution int64) (*ContentAwardService, *memoryLedgerStore, *memoryContentStore, *memoryRewardStore, *memoryAuditStore) {
	t.Helper()
	now := time.Unix(1700000000, 0).UTC()
	ledgerStore := newMemoryLedgerStore()
	ledgerStore.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)}}
	ledgerStore.accounts[accountKey(9, 4, ledger.AssetContribution)] = ledger.Account{ID: 1, UserID: 9, PeriodID: 4, AssetType: ledger.AssetContribution, Balance: contribution, Version: 1}
	rewardStore := newMemoryRewardStore()
	rewardStore.budgets[budgetKey(4, "content_reward")] = ports.RewardBudget{ID: 2, PeriodID: 4, BudgetType: "content_reward", HardCap: 100}
	content := &memoryContentStore{candidates: map[uint64]ports.ContentCandidate{1: {ID: 1, SourceSystem: "answer-forum", SourceContentID: "42", ContentType: "question", AuthorUserID: 9, PeriodID: 4, Status: ports.ContentCandidatePending, PayloadHash: "hash", CursorValue: "42"}}, awards: make(map[string]ports.ContentAward)}
	audit := &memoryAuditStore{}
	service, err := NewContentAwardService(contentAwardUnit{ledger: ledgerStore, reward: rewardStore, idem: newMemoryIdempotencyStore(), content: content, audit: audit}, ContentAwardConfig{MinPaidContributionMilli: 1000, MaxUserPeriodAmount: 50, MaxDailyAmount: 100, ShadowMode: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, ledgerStore, content, rewardStore, audit
}

func TestContentAwardHasFourGatesAndStableIdempotency(t *testing.T) {
	s, _, content, rewards, audit := newContentAwardFixture(t, 1000)
	command := ContentAwardCommand{CandidateID: 1, AwardVersion: 1, RewardType: "quota", Amount: 10, Reason: "精华回答", ActorType: "admin", ActorID: "op-1", RequestID: "review-1"}
	first, err := s.ReviewAndAward(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 99; i++ {
		if _, err := s.ReviewAndAward(context.Background(), command); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	if first.Grant == nil || first.Award.ActionID != "content_award:question:42:1" || len(content.awards) != 1 || len(rewards.grants) != 1 || len(rewards.outboxes) != 1 || len(audit.logs) != 1 {
		t.Fatalf("result=%+v awards=%d grants=%d outboxes=%d audits=%d", first, len(content.awards), len(rewards.grants), len(rewards.outboxes), len(audit.logs))
	}
	if len(rewardsStorePending(rewards)) != 1 || rewards.budgets[budgetKey(4, "content_reward")].ReservedAmount != 10 {
		t.Fatalf("rewards=%+v budget=%+v", rewards.grants, rewards.budgets[budgetKey(4, "content_reward")])
	}
	changed := command
	changed.Amount = 11
	changed.RequestID = "review-2"
	if _, err := s.ReviewAndAward(context.Background(), changed); !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("changed action error=%v, want conflict", err)
	}
}

func rewardsStorePending(rewards *memoryRewardStore) []ports.RewardGrant {
	var pending []ports.RewardGrant
	for _, grant := range rewards.grants {
		if grant.Status == RewardStatusPending {
			pending = append(pending, grant)
		}
	}
	return pending
}

func TestContentAwardPaidThresholdProducesHonorOnlyRecord(t *testing.T) {
	s, _, content, rewards, audit := newContentAwardFixture(t, 999)
	result, err := s.ReviewAndAward(context.Background(), ContentAwardCommand{CandidateID: 1, AwardVersion: 1, RewardType: "quota", Amount: 10, Reason: "优质内容", ActorType: "admin", ActorID: "op-1", RequestID: "review-1"})
	if err != nil || result.Eligibility != ports.ContentAwardIneligible || len(rewards.grants) != 0 || len(rewards.outboxes) != 0 || content.awards[result.Award.ActionID].Status != ports.ContentAwardIneligible || len(audit.logs) != 1 {
		t.Fatalf("result=%+v err=%v awards=%+v audits=%d", result, err, content.awards, len(audit.logs))
	}
}

func TestContentAwardUserAndDailyCapsLimitWithoutGrant(t *testing.T) {
	s, _, content, rewards, _ := newContentAwardFixture(t, 1000)
	content.awards["existing"] = ports.ContentAward{ActionID: "existing", UserID: 9, PeriodID: 4, Amount: 45, Status: ports.ContentAwardPending}
	result, err := s.ReviewAndAward(context.Background(), ContentAwardCommand{CandidateID: 1, AwardVersion: 1, RewardType: "quota", Amount: 10, Reason: "超额测试", ActorType: "admin", ActorID: "op-1", RequestID: "review-1"})
	if err != nil || result.Eligibility != ports.ContentAwardLimited || len(rewards.grants) != 0 {
		t.Fatalf("result=%+v err=%v grants=%d", result, err, len(rewards.grants))
	}
}

func TestContentAwardDoesNotWriteContributionOrTicketLedger(t *testing.T) {
	s, ledgerStore, _, _, _ := newContentAwardFixture(t, 1000)
	_, err := s.ReviewAndAward(context.Background(), ContentAwardCommand{CandidateID: 1, AwardVersion: 1, RewardType: "quota", Amount: 10, Reason: "不进账", ActorType: "admin", ActorID: "op-1", RequestID: "review-1"})
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has only a derived account and no ledger entries; content
	// award must not manufacture contribution/ticket entries.
	if len(ledgerStore.entries) != 0 {
		t.Fatalf("ledger entries=%d, want 0", len(ledgerStore.entries))
	}
}

type memoryGrantRollback struct {
	calls   int
	grantID uint64
	reason  string
}

func (m *memoryGrantRollback) Rollback(_ context.Context, grantID uint64, reason string) error {
	m.calls++
	m.grantID = grantID
	m.reason = reason
	return nil
}

func TestContentAwardReversalUsesOriginalGrant(t *testing.T) {
	s, ledgerStore, content, rewards, audit := newContentAwardFixture(t, 1000)
	command := ContentAwardCommand{CandidateID: 1, AwardVersion: 1, RewardType: "quota", Amount: 10, Reason: "精华回答", ActorType: "admin", ActorID: "op-1", RequestID: "review-1"}
	if _, err := s.ReviewAndAward(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	rollback := &memoryGrantRollback{}
	idem := newMemoryIdempotencyStore()
	s, err := NewContentAwardService(contentAwardUnit{ledger: ledgerStore, reward: rewards, idem: idem, content: content, audit: audit}, ContentAwardConfig{MinPaidContributionMilli: 1000, MaxUserPeriodAmount: 50, MaxDailyAmount: 100, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}, rollback)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reverse(context.Background(), "content_award:question:42:1", "admin", "op-2", "抄袭撤销", "reverse-1"); err != nil {
		t.Fatal(err)
	}
	if rollback.calls != 1 || rollback.grantID != rewards.grants[0].ID || rollback.reason != "抄袭撤销" || content.awards["content_award:question:42:1"].Status != ports.ContentAwardReversed || len(audit.logs) != 2 {
		t.Fatalf("rollback=%+v award=%+v audits=%d", rollback, content.awards["content_award:question:42:1"], len(audit.logs))
	}
	if err := s.Reverse(context.Background(), "content_award:question:42:1", "admin", "op-2", "抄袭撤销", "reverse-1"); err != nil {
		t.Fatalf("same reversal replay failed: %v", err)
	}
	if rollback.calls != 1 || len(audit.logs) != 2 {
		t.Fatalf("same reversal replay was not idempotent: rollback=%+v audits=%d", rollback, len(audit.logs))
	}
	if err := s.Reverse(context.Background(), "content_award:question:42:1", "admin", "op-2", "不同原因", "reverse-1"); !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("changed reversal payload error=%v, want conflict", err)
	}
}
