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

const (
	RewardStatusPending = "pending"
	OutboxStatusPending = "pending"
	OutboxStatusShadow  = "shadow"
	ActionBudgetType    = "loyalty"
)

var (
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
	ErrInvalidAction         = errors.New("invalid action command")
	ErrInsufficientTickets   = errors.New("insufficient tickets")
	ErrBudgetExceeded        = errors.New("reward budget exceeded")
)

type ActionConfig struct {
	RandomSecret []byte
	ShadowMode   bool
	BudgetType   string
	Now          func() time.Time
}

type ActionCommand struct {
	UserID         uint64
	ActionID       string
	TriggerType    string
	IdempotencyKey string
	PayloadHash    string
}

type ActionResult struct {
	GrantID           string `json:"grant_id"`
	PeriodID          uint64 `json:"period_id"`
	UserID            uint64 `json:"user_id"`
	ActionID          string `json:"action_id"`
	RewardType        string `json:"reward_type"`
	Amount            int64  `json:"amount"`
	RandomValue       string `json:"random_value"`
	ConfigVersion     string `json:"config_version"`
	Status            string `json:"status"`
	TransferableQuota bool   `json:"transferable_quota"`
}

type ActionService struct {
	unit   ports.UnitOfWork
	secret []byte
	cfg    ActionConfig
}

func NewActionService(unit ports.UnitOfWork, cfg ActionConfig) (*ActionService, error) {
	if unit == nil {
		return nil, errors.New("action unit of work is nil")
	}
	if len(cfg.RandomSecret) == 0 {
		return nil, errors.New("action random secret is required")
	}
	if cfg.BudgetType == "" {
		cfg.BudgetType = ActionBudgetType
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ActionService{unit: unit, secret: append([]byte(nil), cfg.RandomSecret...), cfg: cfg}, nil
}

func (s *ActionService) Execute(ctx context.Context, command ActionCommand) (ActionResult, error) {
	if command.UserID == 0 || strings.TrimSpace(command.ActionID) == "" || strings.TrimSpace(command.TriggerType) == "" {
		return ActionResult{}, ErrInvalidAction
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return ActionResult{}, ErrMissingIdempotencyKey
	}
	var result ActionResult
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Period == nil || repos.Idempotency == nil || repos.Reward == nil || repos.Ledger == nil || repos.Account == nil {
			return errors.New("action repositories are not initialized")
		}
		activity, err := repos.Period.FindActiveAt(ctx, s.cfg.Now())
		if err != nil {
			return err
		}
		payloadHash := actionPayloadHash(command, activity.ID, activity.ConfigVersion)
		scope := fmt.Sprintf("pulse_action:%d:%d", activity.ID, command.UserID)
		idempotency, err := repos.Idempotency.GetOrCreateForUpdate(ctx, scope, command.IdempotencyKey, payloadHash)
		if err != nil {
			return err
		}
		if idempotency.PayloadHash != payloadHash {
			return fmt.Errorf("%w: action idempotency payload differs", ledger.ErrIdempotencyConflict)
		}
		if idempotency.ResponseStatus != nil && len(idempotency.ResponseJSON) > 0 {
			if err := json.Unmarshal(idempotency.ResponseJSON, &result); err != nil {
				return fmt.Errorf("decode idempotency response: %w", err)
			}
			return nil
		}
		if existing, findErr := repos.Reward.FindGrantByAction(ctx, activity.ID, command.UserID, command.ActionID); findErr == nil {
			result = actionResultFromGrant(*existing)
			return saveActionIdempotency(ctx, repos.Idempotency, idempotency, result)
		} else if !errors.Is(findErr, ports.ErrNotFound) {
			return findErr
		}

		definitions, err := repos.Reward.ListDefinitions(ctx, activity.ID)
		if err != nil {
			return err
		}
		randomBytes, err := reward.Derive(s.secret, activity.ID, command.UserID, command.ActionID, activity.ConfigVersion)
		if err != nil {
			return err
		}
		definition, err := reward.SelectWeighted(definitions, randomBytes)
		if err != nil {
			return err
		}
		if definition.Amount < 0 {
			return errors.New("reward amount cannot be negative")
		}
		ticketAccount, err := repos.Account.GetOrCreateForUpdate(ctx, command.UserID, activity.ID, ledger.AssetTicket)
		if err != nil {
			return err
		}
		if ticketAccount.Balance < 1 {
			return ErrInsufficientTickets
		}
		budget, err := repos.Reward.GetBudgetForUpdate(ctx, activity.ID, s.cfg.BudgetType)
		if err != nil {
			return err
		}
		if err := reserveBudget(&budget, definition.Amount); err != nil {
			return err
		}

		grantID := reward.GrantID(activity.ID, command.UserID, command.ActionID)
		if _, err := appendEntry(ctx, repos, ledger.Entry{
			UserID: command.UserID, PeriodID: activity.ID, AssetType: ledger.AssetTicket,
			Operation: ledger.OperationTicketSpend, Amount: -1, SourceType: "pulse_action",
			SourceRef: command.ActionID, IdempotencyKey: "ticket-spend:" + grantID,
			PayloadHash: payloadHash, Reason: "pulse action",
		}); err != nil {
			if errors.Is(err, ledger.ErrInvalidEntry) {
				return err
			}
			return err
		}
		grant := ports.RewardGrant{
			GrantID: grantID, PeriodID: activity.ID, UserID: command.UserID, ActionID: command.ActionID,
			TriggerType: command.TriggerType, RewardDefinitionID: definition.ID, RewardType: definition.RewardType,
			Amount: definition.Amount, TransferableQuota: false, RandomValue: reward.RandomHex(randomBytes),
			ConfigVersion: activity.ConfigVersion, Status: RewardStatusPending, SourceRef: grantID,
			Reason: "shadow mode", CreatedAt: s.cfg.Now(),
		}
		persistedGrant, err := repos.Reward.CreateGrant(ctx, grant)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"grant_id": persistedGrant.GrantID, "user_id": persistedGrant.UserID,
			"amount": persistedGrant.Amount, "transferable_quota": false,
			"source_ref": persistedGrant.SourceRef, "reward_type": persistedGrant.RewardType,
		})
		if err != nil {
			return err
		}
		payloadDigest := sha256.Sum256(payload)
		outboxStatus := OutboxStatusPending
		if s.cfg.ShadowMode {
			outboxStatus = OutboxStatusShadow
		}
		if _, err := repos.Reward.CreateOutbox(ctx, ports.SettlementOutbox{
			RewardGrantID: persistedGrant.ID, Operation: "grant", PayloadHash: hex.EncodeToString(payloadDigest[:]),
			PayloadJSON: payload, Status: outboxStatus, NextAttemptAt: s.cfg.Now(), CreatedAt: s.cfg.Now(),
		}); err != nil {
			return err
		}
		if err := repos.Reward.SaveBudget(ctx, budget); err != nil {
			return err
		}
		result = actionResultFromGrant(persistedGrant)
		return saveActionIdempotency(ctx, repos.Idempotency, idempotency, result)
	})
	if err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func actionPayloadHash(command ActionCommand, periodID uint64, configVersion string) string {
	payload, _ := json.Marshal(struct {
		PeriodID      uint64 `json:"period_id"`
		ConfigVersion string `json:"config_version"`
		UserID        uint64 `json:"user_id"`
		ActionID      string `json:"action_id"`
		TriggerType   string `json:"trigger_type"`
	}{periodID, configVersion, command.UserID, command.ActionID, command.TriggerType})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func reserveBudget(budget *ports.RewardBudget, amount int64) error {
	if budget == nil || budget.ID == 0 || budget.Version == math.MaxUint64 || budget.HardCap < 0 || budget.ReservedAmount < 0 || budget.SettledAmount < 0 || amount < 0 {
		return ErrBudgetExceeded
	}
	if budget.ReservedAmount > budget.HardCap-budget.SettledAmount {
		return ErrBudgetExceeded
	}
	available := budget.HardCap - budget.SettledAmount - budget.ReservedAmount
	if amount > available {
		return ErrBudgetExceeded
	}
	budget.ReservedAmount += amount
	budget.Version++
	return nil
}

func saveActionIdempotency(ctx context.Context, repo ports.IdempotencyRepository, record ports.IdempotencyRecord, result ActionResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	status := 201
	record.ResponseStatus = &status
	record.ResponseJSON = payload
	record.ResourceType = "reward_grant"
	record.ResourceID = result.GrantID
	return repo.Save(ctx, record)
}

func actionResultFromGrant(grant ports.RewardGrant) ActionResult {
	return ActionResult{GrantID: grant.GrantID, PeriodID: grant.PeriodID, UserID: grant.UserID, ActionID: grant.ActionID, RewardType: grant.RewardType, Amount: grant.Amount, RandomValue: grant.RandomValue, ConfigVersion: grant.ConfigVersion, Status: grant.Status, TransferableQuota: false}
}
