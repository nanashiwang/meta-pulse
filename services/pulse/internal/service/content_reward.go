package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

var (
	ErrContentCandidateUnavailable = errors.New("content candidate is not awardable")
	ErrContentPaidThreshold        = errors.New("content reward paid threshold is not met")
	ErrContentAwardLimit           = errors.New("content reward limit exceeded")
)

type ContentAwardConfig struct {
	MinPaidContributionMilli int64
	MaxUserPeriodAmount      int64
	MaxDailyAmount           int64
	BudgetType               string
	ConfigVersion            string
	ShadowMode               bool
	Now                      func() time.Time
}

type ContentAwardCommand struct {
	CandidateID  uint64
	AwardVersion uint64
	PeriodID     uint64
	RewardType   string
	Amount       int64
	Reason       string
	ActorType    string
	ActorID      string
	RequestID    string
}

type ContentAwardResult struct {
	Award       ports.ContentAward `json:"award"`
	Grant       *ports.RewardGrant `json:"grant,omitempty"`
	Eligibility string             `json:"eligibility"`
}

type ContentAwardService struct {
	unit     ports.UnitOfWork
	cfg      ContentAwardConfig
	rollback GrantRollbacker
}

type GrantRollbacker interface {
	Rollback(context.Context, uint64, string) error
}

func NewContentAwardService(unit ports.UnitOfWork, cfg ContentAwardConfig, rollback ...GrantRollbacker) (*ContentAwardService, error) {
	if unit == nil {
		return nil, errors.New("content award unit of work is nil")
	}
	if cfg.MinPaidContributionMilli < 0 || cfg.MaxUserPeriodAmount <= 0 || cfg.MaxDailyAmount <= 0 {
		return nil, errors.New("content award limits must be positive")
	}
	if strings.TrimSpace(cfg.BudgetType) == "" {
		cfg.BudgetType = "content_reward"
	}
	if strings.TrimSpace(cfg.ConfigVersion) == "" {
		cfg.ConfigVersion = "content-v1"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	var rollbacker GrantRollbacker
	if len(rollback) > 0 {
		rollbacker = rollback[0]
	}
	return &ContentAwardService{unit: unit, cfg: cfg, rollback: rollbacker}, nil
}

// ReviewAndAward is the only content-to-quota mutation. It combines manual
// approval, four anti-abuse gates, an isolated budget reservation, Grant and
// Outbox creation, and an audit record in one Pulse transaction.
func (s *ContentAwardService) ReviewAndAward(ctx context.Context, command ContentAwardCommand) (ContentAwardResult, error) {
	if err := validateContentAwardCommand(command); err != nil {
		return ContentAwardResult{}, err
	}
	payloadHash := contentAwardPayloadHash(command)
	var result ContentAwardResult
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Content == nil || repos.Account == nil || repos.Reward == nil || repos.Idempotency == nil || repos.Audit == nil {
			return errors.New("content award repositories are not initialized")
		}
		idempotency, err := repos.Idempotency.GetOrCreateForUpdate(ctx, "content_award", command.RequestID, payloadHash)
		if err != nil {
			return err
		}
		if idempotency.PayloadHash != payloadHash {
			return fmt.Errorf("%w: content award request=%s", ledger.ErrIdempotencyConflict, command.RequestID)
		}
		if len(idempotency.ResponseJSON) > 0 {
			return json.Unmarshal(idempotency.ResponseJSON, &result)
		}
		candidate, err := repos.Content.FindCandidateForUpdate(ctx, command.CandidateID)
		if err != nil {
			return err
		}
		if candidate.Status == ports.ContentCandidateRejected || candidate.Status == ports.ContentCandidateDeleted || candidate.AuthorUserID == 0 || candidate.ContentType == "" || candidate.SourceContentID == "" {
			return ErrContentCandidateUnavailable
		}
		periodID := command.PeriodID
		if periodID == 0 {
			periodID = candidate.PeriodID
		}
		if periodID == 0 {
			return fmt.Errorf("%w: candidate has no period", ErrContentCandidateUnavailable)
		}
		actionID := fmt.Sprintf("content_award:%s:%s:%d", candidate.ContentType, candidate.SourceContentID, command.AwardVersion)
		if existing, findErr := repos.Content.FindAwardByAction(ctx, actionID); findErr == nil {
			if existing.Amount != command.Amount || existing.RewardType != command.RewardType || existing.UserID != candidate.AuthorUserID || existing.PeriodID != periodID || existing.Reason != command.Reason {
				return fmt.Errorf("%w: content award action=%s", ledger.ErrIdempotencyConflict, actionID)
			}
			result = ContentAwardResult{Award: *existing, Eligibility: existing.Status}
			return saveContentAwardIdempotency(ctx, repos.Idempotency, idempotency, result)
		} else if !errors.Is(findErr, ports.ErrNotFound) {
			return findErr
		}
		if err := repos.Content.ReviewCandidate(ctx, candidate.ID, ports.ContentCandidateApproved, command.ActorType, command.ActorID, command.Reason, s.cfg.Now()); err != nil {
			return err
		}

		lifetimeContribution, err := lifetimeContribution(ctx, repos.Account, candidate.AuthorUserID)
		if err != nil {
			return err
		}
		award := ports.ContentAward{CandidateID: candidate.ID, AwardVersion: command.AwardVersion, ActionID: actionID, PeriodID: periodID, UserID: candidate.AuthorUserID, Amount: command.Amount, RewardType: command.RewardType, BudgetType: s.cfg.BudgetType, Status: ports.ContentAwardPending, Reason: command.Reason, CreatedAt: s.cfg.Now()}
		if lifetimeContribution < s.cfg.MinPaidContributionMilli {
			award.Status = ports.ContentAwardIneligible
			if _, err := repos.Content.CreateAward(ctx, award); err != nil {
				return err
			}
			result = ContentAwardResult{Award: award, Eligibility: award.Status}
			if err := appendContentAudit(ctx, repos.Audit, command, candidate, award, "paid_threshold", s.cfg.Now()); err != nil {
				return err
			}
			return saveContentAwardIdempotency(ctx, repos.Idempotency, idempotency, result)
		}
		userTotal, err := repos.Content.SumUserActiveAwards(ctx, candidate.AuthorUserID, periodID)
		if err != nil {
			return err
		}
		dailyTotal, err := repos.Content.SumDailyActiveAwards(ctx, s.cfg.Now())
		if err != nil {
			return err
		}
		if userTotal > s.cfg.MaxUserPeriodAmount-command.Amount || dailyTotal > s.cfg.MaxDailyAmount-command.Amount {
			award.Status = ports.ContentAwardLimited
			if _, err := repos.Content.CreateAward(ctx, award); err != nil {
				return err
			}
			result = ContentAwardResult{Award: award, Eligibility: award.Status}
			if err := appendContentAudit(ctx, repos.Audit, command, candidate, award, "limit", s.cfg.Now()); err != nil {
				return err
			}
			return saveContentAwardIdempotency(ctx, repos.Idempotency, idempotency, result)
		}
		budget, err := repos.Reward.GetBudgetForUpdate(ctx, periodID, s.cfg.BudgetType)
		if err != nil {
			return err
		}
		if err := reserveBudget(&budget, command.Amount); err != nil {
			return fmt.Errorf("%w: %v", ErrContentAwardLimit, err)
		}
		grantID := reward.GrantID(periodID, candidate.AuthorUserID, actionID)
		grant := ports.RewardGrant{GrantID: grantID, PeriodID: periodID, UserID: candidate.AuthorUserID, ActionID: actionID, TriggerType: "content", RewardDefinitionID: 0, RewardType: command.RewardType, Amount: command.Amount, TransferableQuota: false, BudgetType: s.cfg.BudgetType, RandomValue: hashString(actionID), ConfigVersion: s.cfg.ConfigVersion, Status: RewardStatusPending, SourceRef: grantID, Reason: command.Reason, CreatedAt: s.cfg.Now()}
		persistedGrant, err := repos.Reward.CreateGrant(ctx, grant)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(settlementPayload{GrantID: persistedGrant.GrantID, UserID: persistedGrant.UserID, Amount: persistedGrant.Amount, SourceRef: persistedGrant.SourceRef, RewardType: persistedGrant.RewardType})
		if err != nil {
			return err
		}
		status := OutboxStatusPending
		if s.cfg.ShadowMode {
			status = OutboxStatusShadow
		}
		if _, err := repos.Reward.CreateOutbox(ctx, ports.SettlementOutbox{RewardGrantID: persistedGrant.ID, Operation: "grant", PayloadHash: canonicalJSONHash(payload), PayloadJSON: payload, Status: status, NextAttemptAt: s.cfg.Now(), CreatedAt: s.cfg.Now()}); err != nil {
			return err
		}
		if err := repos.Reward.SaveBudget(ctx, budget); err != nil {
			return err
		}
		award.GrantID = persistedGrant.GrantID
		if _, err := repos.Content.CreateAward(ctx, award); err != nil {
			return err
		}
		result = ContentAwardResult{Award: award, Grant: &persistedGrant, Eligibility: "eligible"}
		if err := appendContentAudit(ctx, repos.Audit, command, candidate, award, "approved", s.cfg.Now()); err != nil {
			return err
		}
		return saveContentAwardIdempotency(ctx, repos.Idempotency, idempotency, result)
	})
	if err != nil {
		return ContentAwardResult{}, err
	}
	return result, nil
}

func lifetimeContribution(ctx context.Context, accounts ports.AccountRepository, userID uint64) (int64, error) {
	rows, err := accounts.ListForUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, account := range rows {
		if account.AssetType != ledger.AssetContribution {
			continue
		}
		if account.Balance > 0 && total > math.MaxInt64-account.Balance {
			return 0, errors.New("lifetime contribution overflow")
		}
		if account.Balance < 0 && total < math.MinInt64-account.Balance {
			return 0, errors.New("lifetime contribution overflow")
		}
		total += account.Balance
	}
	return total, nil
}

// Reverse marks a content award as reversed only after the original Grant is
// rolled back through the normal Benefit source_ref. It never creates a new
// source_ref or a compensating content award.
func (s *ContentAwardService) Reverse(ctx context.Context, actionID, actorType, actorID, reason, requestID string) error {
	if s.rollback == nil {
		return errors.New("content award rollback is not configured")
	}
	if strings.TrimSpace(actionID) == "" || strings.TrimSpace(actorType) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid content award reversal")
	}
	payloadHash := contentReversalPayloadHash(actionID, actorType, actorID, reason)

	// Reserve the operator idempotency key before the external rollback. The
	// response is saved only after local state commits, so a crash between the
	// new-api call and the local commit can safely retry the same source_ref.
	var grantID uint64
	var alreadyReversed bool
	if err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Content == nil || repos.Reward == nil || repos.Idempotency == nil {
			return errors.New("content reversal repositories are not initialized")
		}
		idempotency, err := repos.Idempotency.GetOrCreateForUpdate(ctx, "content_reward_reversal", requestID, payloadHash)
		if err != nil {
			return err
		}
		if idempotency.PayloadHash != payloadHash {
			return fmt.Errorf("%w: content reversal request=%s", ledger.ErrIdempotencyConflict, requestID)
		}
		if idempotency.ResponseStatus != nil && len(idempotency.ResponseJSON) > 0 {
			return nil
		}

		var found *ports.ContentAward
		if locker, ok := repos.Content.(interface {
			FindAwardByActionForUpdate(context.Context, string) (*ports.ContentAward, error)
		}); ok {
			found, err = locker.FindAwardByActionForUpdate(ctx, actionID)
		} else {
			found, err = repos.Content.FindAwardByAction(ctx, actionID)
		}
		if err != nil {
			return err
		}
		if found.Status == ports.ContentAwardReversed {
			alreadyReversed = true
			return saveContentReversalIdempotency(ctx, repos.Idempotency, idempotency, actionID)
		}
		if found.GrantID == "" {
			return ErrContentCandidateUnavailable
		}
		grant, err := repos.Reward.FindGrantByAction(ctx, found.PeriodID, found.UserID, found.ActionID)
		if err != nil {
			return err
		}
		grantID = grant.ID
		return nil
	}); err != nil {
		return err
	}
	if alreadyReversed || grantID == 0 {
		// The award was already reversed, or the same request has already been
		// completed. Both paths are intentionally idempotent.
		return nil
	}

	// Do not hold a Pulse transaction open while calling new-api; Benefit is an
	// independent idempotent boundary keyed by the original grant source_ref.
	if err := s.rollback.Rollback(ctx, grantID, reason); err != nil {
		return err
	}

	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Content == nil || repos.Audit == nil || repos.Idempotency == nil {
			return errors.New("content reversal repositories are not initialized")
		}
		idempotency, err := repos.Idempotency.GetOrCreateForUpdate(ctx, "content_reward_reversal", requestID, payloadHash)
		if err != nil {
			return err
		}
		if idempotency.PayloadHash != payloadHash {
			return fmt.Errorf("%w: content reversal request=%s", ledger.ErrIdempotencyConflict, requestID)
		}
		if idempotency.ResponseStatus != nil && len(idempotency.ResponseJSON) > 0 {
			return nil
		}

		var found *ports.ContentAward
		if locker, ok := repos.Content.(interface {
			FindAwardByActionForUpdate(context.Context, string) (*ports.ContentAward, error)
		}); ok {
			found, err = locker.FindAwardByActionForUpdate(ctx, actionID)
		} else {
			found, err = repos.Content.FindAwardByAction(ctx, actionID)
		}
		if err != nil {
			return err
		}
		if found.Status == ports.ContentAwardReversed {
			return saveContentReversalIdempotency(ctx, repos.Idempotency, idempotency, actionID)
		}
		if err := repos.Content.UpdateAwardStatus(ctx, actionID, ports.ContentAwardReversed); err != nil {
			return err
		}
		after, _ := json.Marshal(map[string]any{"action_id": actionID, "status": ports.ContentAwardReversed})
		if err := repos.Audit.Append(ctx, ports.AuditLog{ActorType: actorType, ActorID: actorID, Action: "content_reward_reversal", ResourceType: "content_award", ResourceID: actionID, Reason: reason, AfterJSON: after, RequestID: requestID, CreatedAt: s.cfg.Now()}); err != nil {
			return err
		}
		return saveContentReversalIdempotency(ctx, repos.Idempotency, idempotency, actionID)
	})
}

func contentReversalPayloadHash(actionID, actorType, actorID, reason string) string {
	payload, _ := json.Marshal(struct {
		ActionID  string `json:"action_id"`
		ActorType string `json:"actor_type"`
		ActorID   string `json:"actor_id"`
		Reason    string `json:"reason"`
	}{actionID, actorType, actorID, reason})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func saveContentReversalIdempotency(ctx context.Context, repo ports.IdempotencyRepository, record ports.IdempotencyRecord, actionID string) error {
	payload, err := json.Marshal(map[string]any{"action_id": actionID, "status": ports.ContentAwardReversed})
	if err != nil {
		return err
	}
	status := 204
	record.ResponseStatus = &status
	record.ResponseJSON = payload
	record.ResourceType = "content_award_reversal"
	record.ResourceID = actionID
	return repo.Save(ctx, record)
}

func validateContentAwardCommand(command ContentAwardCommand) error {
	if command.CandidateID == 0 || command.AwardVersion == 0 || command.Amount <= 0 || strings.TrimSpace(command.RewardType) == "" || strings.TrimSpace(command.Reason) == "" || strings.TrimSpace(command.ActorType) == "" || strings.TrimSpace(command.ActorID) == "" || strings.TrimSpace(command.RequestID) == "" {
		return errors.New("invalid content award command")
	}
	return nil
}

func contentAwardPayloadHash(command ContentAwardCommand) string {
	payload, _ := json.Marshal(command)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func saveContentAwardIdempotency(ctx context.Context, repo ports.IdempotencyRepository, record ports.IdempotencyRecord, result ContentAwardResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	status := 201
	record.ResponseStatus = &status
	record.ResponseJSON = payload
	record.ResourceType = "content_award"
	record.ResourceID = result.Award.ActionID
	return repo.Save(ctx, record)
}

func appendContentAudit(ctx context.Context, audit ports.AuditRepository, command ContentAwardCommand, candidate *ports.ContentCandidate, award ports.ContentAward, decision string, at time.Time) error {
	after, err := json.Marshal(struct {
		Decision    string             `json:"decision"`
		CandidateID uint64             `json:"candidate_id"`
		Award       ports.ContentAward `json:"award"`
	}{decision, candidate.ID, award})
	if err != nil {
		return err
	}
	return audit.Append(ctx, ports.AuditLog{ActorType: command.ActorType, ActorID: command.ActorID, Action: "content_reward_review", ResourceType: "content_candidate", ResourceID: fmt.Sprintf("%d", candidate.ID), Reason: command.Reason, AfterJSON: after, RequestID: command.RequestID, CreatedAt: at})
}
