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
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
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
	ErrActionsUnavailable    = errors.New("new pulse actions are unavailable")
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
	ErrInvalidAction         = errors.New("invalid action command")
	ErrInsufficientTickets   = errors.New("insufficient tickets")
	ErrBudgetExceeded        = errors.New("reward budget exceeded")
)

type ActionConfig struct {
	RandomSecret           []byte
	ShadowMode             bool
	DisableNewActions      bool
	RequireVerifiedFunding bool
	BudgetType             string
	Now                    func() time.Time
}

type ActionCommand struct {
	ProtocolVersion int
	Selection       string
	UserID          uint64
	ActionID        string
	TriggerType     string
	IdempotencyKey  string
	PayloadHash     string
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
	var refusal error
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
			return replayActionResponse(idempotency, &result, &refusal)
		}

		refuse := func(cause error) error {
			code := actionRefusalCode(cause)
			if code == "" {
				return cause
			}
			if err := saveActionRefusal(ctx, repos.Idempotency, actionIdentity, code); err != nil {
				return err
			}
			if err := saveActionRefusal(ctx, repos.Idempotency, idempotency, code); err != nil {
				return err
			}
			refusal = cause
			return nil
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
			if command.ProtocolVersion != 0 || command.Selection != "" {
				return fmt.Errorf("%w: legacy selection cannot change", ledger.ErrIdempotencyConflict)
			}
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
			if actionIdentity.ResourceType == "action_refusal" {
				if len(legacy) > 0 {
					return ledger.ErrIdempotencyConflict
				}
				if err := replayActionResponse(actionIdentity, &result, &refusal); err != nil {
					return err
				}
				return saveActionRefusal(ctx, repos.Idempotency, idempotency, actionRefusalCode(refusal))
			}
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
			if command.ProtocolVersion != 0 || command.Selection != "" {
				return fmt.Errorf("%w: historical selection unavailable", ledger.ErrIdempotencyConflict)
			}
			result = actionResultFromGrant(grants[0])
			return saveResult()
		}
		if command.ProtocolVersion != 3 || command.Selection == "" {
			return refuse(ErrSelectionRequired)
		}
		if s.cfg.DisableNewActions {
			return refuse(ErrActionsUnavailable)
		}
		choice, err := verifySelection(s.secret, command.UserID, command.Selection)
		if err != nil {
			return refuse(err)
		}
		if err := lockRewardRule(ctx, repos.Idempotency); err != nil {
			return err
		}
		now := s.cfg.Now()
		activity, source, lot, err := selectedTicket(ctx, repos, choice, command.UserID, now)
		if err != nil {
			return refuse(err)
		}
		if s.cfg.RequireVerifiedFunding && (activity.FundingPolicy != period.VerifiedPaidFunding || activity.TicketThresholdMilli <= 0 || source.FundingPolicy != period.VerifiedPaidFunding || source.TicketThresholdMilli <= 0) {
			return refuse(ErrActionsUnavailable)
		}
		definitions, err := repos.Reward.ListDefinitions(ctx, activity.ID)
		if err != nil {
			return err
		}
		if err := validateRewardDefinitions(definitions, activity.ConfigVersion); err != nil {
			return err
		}
		if lot != nil && !now.Before(lot.QuotaExpiresAt) {
			definitions = experienceOnly(definitions)
		}
		randomBytes, err := reward.Derive(s.secret, activity.ID, command.UserID, command.ActionID, activity.ConfigVersion)
		if err != nil {
			return err
		}
		definition, err := reward.SelectWeighted(definitions, randomBytes)
		if err != nil {
			return err
		}
		ticketAccount, err := repos.Account.GetOrCreateForUpdate(ctx, command.UserID, source.ID, ledger.AssetTicket)
		if err != nil {
			return err
		}
		if ticketAccount.Balance < 1 {
			return refuse(ErrInsufficientTickets)
		}

		// Lock units in a fixed order and stop the whole pool if either budget
		// cannot cover its largest prize; never change the advertised odds.
		budgets := map[string]ports.RewardBudget{}
		for _, kind := range []string{ActionBudgetType, ExperienceRewardType} {
			var maxPrize int64
			for _, d := range definitions {
				if d.Enabled && rewardBudget(d.RewardType) == kind && d.Amount > maxPrize {
					maxPrize = d.Amount
				}
			}
			if maxPrize == 0 {
				continue
			}
			b, err := repos.Reward.GetBudgetForUpdate(ctx, activity.ID, kind)
			if errors.Is(err, ports.ErrNotFound) {
				return refuse(ErrBudgetExceeded)
			}
			if err != nil {
				return err
			}
			if !b.CanReserve(maxPrize) {
				return refuse(ErrBudgetExceeded)
			}
			budgets[kind] = b
		}
		budget := budgets[rewardBudget(definition.RewardType)]

		if err := reserveBudget(&budget, definition.Amount); err != nil {
			return refuse(err)
		}

		// User/account/budget locks may have waited across a deadline. Recheck
		// after all of them, immediately before the first financial write.
		decisionAt := s.cfg.Now()
		if !activity.Contains(decisionAt) || (!source.Continuous && !source.Contains(decisionAt)) || (lot != nil && (!decisionAt.Before(lot.QuotaExpiresAt)) != choice.ExperienceOnly) {
			return refuse(ErrSelectionChanged)
		}
		grantID := reward.GrantID(activity.ID, command.UserID, command.ActionID)
		metadata := map[string]any{"selection_version": 3, "reward_period_id": activity.ID, "reward_config_version": activity.ConfigVersion, "experience_only": choice.ExperienceOnly}
		if lot != nil {
			metadata["ticket_lot_version"] = 1
			metadata["mint_entry_id"] = lot.MintEntryID
			metadata["ticket_lot_id"] = lot.ID
		}
		ticketMetadata, _ := json.Marshal(metadata)
		spentEntry, err := appendEntry(ctx, repos, ledger.Entry{
			UserID: command.UserID, PeriodID: source.ID, AssetType: ledger.AssetTicket,
			Operation: ledger.OperationTicketSpend, Amount: -1, SourceType: "pulse_action",
			SourceRef: command.ActionID, IdempotencyKey: "ticket-spend:" + grantID,
			PayloadHash: payloadHash, Reason: "pulse action", MetadataJSON: ticketMetadata,
		})
		if err != nil {
			if errors.Is(err, ledger.ErrInvalidEntry) {
				return err
			}
			return err
		}
		if lot != nil {
			if err := repos.Tickets.Spend(ctx, *lot, spentEntry.ID); err != nil {
				return err
			}
		}
		stat, err := repos.UserPeriod.GetOrCreateForUpdate(ctx, command.UserID, source.ID)
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
			Amount: definition.Amount, TransferableQuota: false, BudgetType: rewardBudget(definition.RewardType), RandomValue: reward.RandomHex(randomBytes),
			ConfigVersion: activity.ConfigVersion, Status: RewardStatusPending, SourceRef: grantID,
			Reason: "pulse action", CreatedAt: decisionAt,
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
		if definition.RewardType == ExperienceRewardType {
			outboxStatus = "community_pending"
		}
		if s.cfg.ShadowMode {
			outboxStatus = OutboxStatusShadow
		}
		if _, err := repos.Reward.CreateOutbox(ctx, ports.SettlementOutbox{
			RewardGrantID: persistedGrant.ID, Operation: "grant", PayloadHash: canonicalJSONHash(payload),
			PayloadJSON: payload, Status: outboxStatus, NextAttemptAt: decisionAt, CreatedAt: decisionAt,
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
	return result, refusal
}

func actionRequestHash(command ActionCommand) string {
	if command.ProtocolVersion != 0 || command.Selection != "" {
		payload, _ := json.Marshal(struct {
			UserID          uint64 `json:"user_id"`
			ActionID        string `json:"action_id"`
			TriggerType     string `json:"trigger_type"`
			ProtocolVersion int    `json:"protocol_version"`
			Selection       string `json:"selection"`
		}{command.UserID, command.ActionID, command.TriggerType, command.ProtocolVersion, command.Selection})
		return canonicalJSONHash(payload)
	}
	// Preserve v1 bytes exactly; no new defaults may reinterpret old requests.

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
		if !definition.Valid() || definition.Amount <= 0 || (definition.RewardType == ExperienceRewardType && definition.Amount > 1000000) {
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
	if budget == nil || budget.ID == 0 || budget.Version == math.MaxUint64 || !budget.CanReserve(amount) {
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

// Expired tickets use the experience sub-pool of the current reward rule.
func experienceOnly(definitions []reward.Definition) []reward.Definition {
	result := make([]reward.Definition, 0, len(definitions))
	for _, d := range definitions {
		if d.RewardType == ExperienceRewardType {
			result = append(result, d)
		}
	}
	return result
}
