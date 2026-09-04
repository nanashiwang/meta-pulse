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
	ActionTriggerType   = "pulse"
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
	cfg.BudgetType = strings.TrimSpace(cfg.BudgetType)
	if cfg.BudgetType == "" {
		cfg.BudgetType = ActionBudgetType
	}
	if cfg.BudgetType != ActionBudgetType {
		return nil, errors.New("action budget type must be loyalty")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ActionService{unit: unit, secret: append([]byte(nil), cfg.RandomSecret...), cfg: cfg}, nil
}

func (s *ActionService) Execute(ctx context.Context, command ActionCommand) (ActionResult, error) {
	command.ActionID = strings.TrimSpace(command.ActionID)
	command.TriggerType = strings.TrimSpace(command.TriggerType)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.UserID == 0 || command.ActionID == "" || command.TriggerType != ActionTriggerType ||
		!validDBText(command.ActionID, 191) {
		return ActionResult{}, ErrInvalidAction
	}
	if command.IdempotencyKey == "" {
		return ActionResult{}, ErrMissingIdempotencyKey
	}
	if !validDBText(command.IdempotencyKey, 191) {
		return ActionResult{}, ErrInvalidAction
	}
	var result ActionResult
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Period == nil || repos.Idempotency == nil || repos.Reward == nil || repos.Ledger == nil || repos.Account == nil || repos.UserPeriod == nil {
			return errors.New("action repositories are not initialized")
		}
		// Request identity must not depend on the wall clock, active period or
		// mutable runtime config. A committed response is replayable forever.
		payloadHash := actionRequestHash(command)
		// Lock action identity before request identity, consistently across calls.
		// Its scope sorts before the request scope: inserting it later can form
		// an InnoDB duplicate-key gap-lock cycle with concurrent request replays.
		// Changing only the request key must never create a second action.
		actionIdentity, err := repos.Idempotency.GetOrCreateForUpdate(ctx, fmt.Sprintf("pulse_action_identity:%d", command.UserID), command.ActionID, payloadHash)
		if err != nil {
			return err
		}
		if actionIdentity.PayloadHash != payloadHash {
			return fmt.Errorf("%w: action identity payload differs", ledger.ErrIdempotencyConflict)
		}

		idempotency, err := repos.Idempotency.GetOrCreateForUpdate(ctx, fmt.Sprintf("pulse_action_request:%d", command.UserID), command.IdempotencyKey, payloadHash)
		if err != nil {
			return err
		}
		if idempotency.PayloadHash != payloadHash {
			return fmt.Errorf("%w: action idempotency payload differs", ledger.ErrIdempotencyConflict)
		}
		if idempotency.ResponseStatus != nil && len(idempotency.ResponseJSON) > 0 {
			return json.Unmarshal(idempotency.ResponseJSON, &result)
		}

		// Import old period-scoped requests without rewriting their history. If
		// an old key was reused across periods, fail closed for operator review.
		legacy, err := repos.Idempotency.LegacyActionRequests(ctx, command.UserID, command.IdempotencyKey)
		if err != nil {
			return err
		}
		if len(legacy) > 1 {
			return fmt.Errorf("%w: ambiguous legacy action request", ledger.ErrIdempotencyConflict)
		}
		if len(legacy) == 1 {
			old := legacy[0]
			if old.ResponseStatus == nil || json.Unmarshal(old.ResponseJSON, &result) != nil ||
				result.UserID != command.UserID || result.ActionID != command.ActionID || result.GrantID == "" ||
				old.Scope != fmt.Sprintf("pulse_action:%d:%d", result.PeriodID, command.UserID) ||
				old.PayloadHash != actionPayloadHash(command, result.PeriodID, result.ConfigVersion) {
				return fmt.Errorf("%w: legacy action payload differs or is incomplete", ledger.ErrIdempotencyConflict)
			}
		}

		saveResult := func() error {
			if err := saveActionIdempotency(ctx, repos.Idempotency, actionIdentity, result); err != nil {
				return err
			}
			return saveActionIdempotency(ctx, repos.Idempotency, idempotency, result)
		}
		if actionIdentity.ResponseStatus != nil && len(actionIdentity.ResponseJSON) > 0 {
			var cached ActionResult
			if err := json.Unmarshal(actionIdentity.ResponseJSON, &cached); err != nil {
				return fmt.Errorf("decode action response: %w", err)
			}
			if len(legacy) == 1 {
				if result.GrantID != cached.GrantID {
					return fmt.Errorf("%w: legacy/action identity mismatch", ledger.ErrIdempotencyConflict)
				}
				// A new alias may have been recovered from an already settled
				// grant. Preserve this old key's original response verbatim.
			} else {
				result = cached
			}
			return saveActionIdempotency(ctx, repos.Idempotency, idempotency, result)
		}
		grants, err := repos.Reward.ListPulseGrantsByAction(ctx, command.UserID, command.ActionID)
		if err != nil {
			return err
		}
		if len(grants) > 1 {
			return fmt.Errorf("%w: ambiguous historical action grants", ledger.ErrIdempotencyConflict)
		}
		if len(legacy) == 1 {
			if len(grants) != 1 || grants[0].GrantID != result.GrantID {
				return fmt.Errorf("%w: legacy action grant does not match", ledger.ErrIdempotencyConflict)
			}
			return saveResult()
		}
		if len(grants) == 1 {
			result = actionResultFromGrant(grants[0])
			return saveResult()
		}
		activity, err := repos.Period.FindActiveAt(ctx, s.cfg.Now())
		if err != nil {
			return err
		}

		definitions, err := repos.Reward.ListDefinitions(ctx, activity.ID)
		if err != nil {
			return err
		}
		if err := validateRewardDefinitions(definitions, activity.ConfigVersion); err != nil {
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
		stat, err := repos.UserPeriod.GetOrCreateForUpdate(ctx, command.UserID, activity.ID)
		if err != nil {
			return err
		}
		if stat.SpentTickets < 0 || stat.SpentTickets == math.MaxInt64 || stat.Version == math.MaxUint64 {
			return errors.New("user period spent tickets overflow")
		}
		stat.SpentTickets++
		stat.Version++
		if err := repos.UserPeriod.Save(ctx, stat); err != nil {
			return err
		}
		grant := ports.RewardGrant{
			GrantID: grantID, PeriodID: activity.ID, UserID: command.UserID, ActionID: command.ActionID,
			TriggerType: command.TriggerType, RewardDefinitionID: definition.ID, RewardType: definition.RewardType,
			Amount: definition.Amount, TransferableQuota: false, BudgetType: s.cfg.BudgetType, RandomValue: reward.RandomHex(randomBytes),
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
		outboxStatus := OutboxStatusPending
		if s.cfg.ShadowMode {
			outboxStatus = OutboxStatusShadow
		}
		if _, err := repos.Reward.CreateOutbox(ctx, ports.SettlementOutbox{
			RewardGrantID: persistedGrant.ID, Operation: "grant", PayloadHash: canonicalJSONHash(payload),
			PayloadJSON: payload, Status: outboxStatus, NextAttemptAt: s.cfg.Now(), CreatedAt: s.cfg.Now(),
		}); err != nil {
			return err
		}
		if err := repos.Reward.SaveBudget(ctx, budget); err != nil {
			return err
		}
		result = actionResultFromGrant(persistedGrant)
		return saveResult()
	})
	if err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func actionRequestHash(command ActionCommand) string {
	payload, _ := json.Marshal(struct {
		UserID      uint64 `json:"user_id"`
		ActionID    string `json:"action_id"`
		TriggerType string `json:"trigger_type"`
	}{command.UserID, command.ActionID, command.TriggerType})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// actionPayloadHash is retained only to verify pre-upgrade request fingerprints.
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

func validateRewardDefinitions(definitions []reward.Definition, configVersion string) error {
	for _, definition := range definitions {
		if !definition.Enabled {
			continue
		}
		// A malformed enabled row must fail the whole immutable probability
		// table. Silently skipping it would renormalize every other weight.
		if !definition.Valid() || definition.Amount <= 0 {
			return fmt.Errorf("invalid enabled reward definition %d", definition.ID)
		}
		if definition.ConfigVersion != configVersion {
			return fmt.Errorf("reward definition %d config version mismatch", definition.ID)
		}
		if definition.TransferableQuota {
			return fmt.Errorf("reward definition %d requests transferable quota", definition.ID)
		}
	}
	return nil
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
