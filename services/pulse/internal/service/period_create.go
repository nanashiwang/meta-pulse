package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// PeriodLength is the fixed campaign window from docs/ARCHITECTURE.md §9.
const PeriodLength = 10 * 24 * time.Hour

// PeriodRuleSpec is one contribution rule of the period being created. An
// empty ModelPattern with a nil ChannelID is the catch-all rule.
type PeriodRuleSpec struct {
	Key           string
	Priority      int
	ModelPattern  string
	ChannelID     *uint64
	Eligible      bool
	MultiplierBps int32
}

// PeriodCreateCommand describes one new period and the rules frozen with it.
//
// Rules are not optional. A period without a matching rule still ingests
// events, but records every one of them as ineligible with zero contribution
// (see usage_ingest.go), and invariant #11 forbids repairing that in place
// once the period is active.
type PeriodCreateCommand struct {
	RequestID            string
	ActorType            string
	ActorID              string
	Key                  string
	StartsAt             time.Time
	Timezone             string
	ConfigVersion        string
	RandomVersion        string
	Rules                []PeriodRuleSpec
	Rewards              []PeriodRewardSpec
	RewardBudget         int64
	TicketThresholdMilli int64
	Activate             bool
	Reason               string
}

// PeriodRewardSpec accepts only integer quota rewards. Transferability and
// funding policy are fixed by the service and cannot be selected by operators.
type PeriodRewardSpec struct {
	Key    string `json:"key"`
	Amount int64  `json:"amount"`
	Weight uint64 `json:"weight"`
}

type PeriodCreateResult struct {
	PeriodID             uint64    `json:"period_id"`
	Key                  string    `json:"period_key"`
	Status               string    `json:"status"`
	StartsAt             time.Time `json:"starts_at"`
	EndsAt               time.Time `json:"ends_at"`
	RuleCount            int       `json:"rule_count"`
	RewardCount          int       `json:"reward_count"`
	RewardBudget         int64     `json:"reward_budget"`
	FundingPolicy        string    `json:"funding_policy"`
	TicketThresholdMilli int64     `json:"ticket_threshold_milli"`
}

type PeriodCreateService struct {
	unit ports.UnitOfWork
	now  func() time.Time
}

func NewPeriodCreateService(unit ports.UnitOfWork, now func() time.Time) (*PeriodCreateService, error) {
	if unit == nil {
		return nil, errors.New("period create unit of work is nil")
	}
	if now == nil {
		now = time.Now
	}
	return &PeriodCreateService{unit: unit, now: now}, nil
}

// Create writes the period, its rules and the audit record in one transaction,
// optionally activating it. Activation happens last so a failure anywhere
// leaves a draft rather than a live period with partial rules.
func (s *PeriodCreateService) Create(ctx context.Context, command PeriodCreateCommand) (PeriodCreateResult, error) {
	command, err := normalizePeriodCreateCommand(command)
	if err != nil {
		return PeriodCreateResult{}, fmt.Errorf("%w: %v", ErrInvalidPeriod, err)
	}
	endsAt := command.StartsAt.Add(PeriodLength)

	payload, _ := json.Marshal(command)
	digest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(digest[:])
	var result PeriodCreateResult
	err = s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.PeriodAdmin == nil || repos.EconomicsAdmin == nil || repos.Audit == nil {
			return errors.New("period create repositories are not initialized")
		}
		if len(command.Rewards) > 0 && repos.RewardAdmin == nil {
			return errors.New("reward setup repository is not initialized")
		}
		if repos.Idempotency == nil {
			return errors.New("period idempotency repository is not initialized")
		}
		// Serialize all CLI and web creations before checking intervals. The
		// permanent row is only a transaction mutex, never an accounting fact.
		if _, err := repos.Idempotency.GetOrCreateForUpdate(ctx, "period_create_lock", "global", "447cc9dbdc73a33ea5be9cef405e81ef56c4e23b9202238aee120a0078c2fb2a"); err != nil {
			return err
		}
		var replay ports.IdempotencyRecord
		if command.RequestID != "" {
			var err error
			replay, err = repos.Idempotency.GetOrCreateForUpdate(ctx, "period_create:"+command.ActorType+":"+command.ActorID, command.RequestID, payloadHash)
			if err != nil {
				return err
			}
			if replay.PayloadHash != payloadHash {
				return ports.ErrConflict
			}
			if len(replay.ResponseJSON) > 0 {
				return json.Unmarshal(replay.ResponseJSON, &result)
			}
		}
		if _, err := repos.PeriodAdmin.FindByKeyForUpdate(ctx, command.Key); err == nil {
			return fmt.Errorf("%w: period key already exists", ports.ErrConflict)
		} else if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		overlapping, err := repos.PeriodAdmin.ListOverlapping(ctx, command.StartsAt, endsAt)
		if err != nil {
			return err
		}
		if len(overlapping) > 0 {
			return fmt.Errorf("%w: period %s overlaps existing period %s", ports.ErrConflict, command.Key, overlapping[0].Key)
		}
		fundingPolicy := "legacy"
		if len(command.Rewards) > 0 {
			fundingPolicy = period.VerifiedPaidFunding
		}
		created, err := repos.PeriodAdmin.Create(ctx, period.Period{
			Key: command.Key, Status: period.StatusDraft,
			StartsAt: command.StartsAt, EndsAt: endsAt, Timezone: command.Timezone,
			ConfigVersion: command.ConfigVersion, RandomVersion: command.RandomVersion,
			FundingPolicy: fundingPolicy, TicketThresholdMilli: command.TicketThresholdMilli,
		})
		if err != nil {
			return err
		}
		rules := make([]economics.Rule, 0, len(command.Rules))
		for _, spec := range command.Rules {
			rule, err := repos.EconomicsAdmin.CreateRule(ctx, created.ID, economics.Rule{
				Key: spec.Key, Priority: spec.Priority, ModelPattern: spec.ModelPattern,
				ChannelID: spec.ChannelID, Eligible: spec.Eligible,
				MultiplierBps: money.Bps(spec.MultiplierBps), ConfigVersion: command.ConfigVersion,
			})
			if err != nil {
				return err
			}
			rules = append(rules, rule)
		}
		// Validate through the same path the ingest hot loop uses, so a period
		// can never go active carrying rules that would fail every batch.
		if err := economics.ValidateRules(rules, created.ConfigVersion); err != nil {
			return err
		}
		if len(command.Rewards) > 0 {
			definitions := make([]reward.Definition, 0, len(command.Rewards))
			for _, spec := range command.Rewards {
				definition, err := repos.RewardAdmin.CreateDefinition(ctx, created.ID, reward.Definition{RewardKey: spec.Key, RewardType: "newapi_quota", Amount: spec.Amount, Weight: spec.Weight, Enabled: true, ConfigVersion: created.ConfigVersion})
				if err != nil {
					return err
				}
				definitions = append(definitions, definition)
			}
			if _, err := reward.SelectWeighted(definitions, [32]byte{}); err != nil {
				return err
			}
			if _, err := repos.RewardAdmin.CreateBudget(ctx, ports.RewardBudget{PeriodID: created.ID, BudgetType: "loyalty", HardCap: command.RewardBudget}); err != nil {
				return err
			}
		}
		status := period.StatusDraft
		if command.Activate {
			if err := repos.PeriodAdmin.Transition(ctx, created.ID, period.StatusDraft, period.StatusActive, s.now()); err != nil {
				return err
			}
			status = period.StatusActive
		}
		afterJSON, err := json.Marshal(struct {
			PeriodKey            string             `json:"period_key"`
			Status               string             `json:"status"`
			StartsAt             time.Time          `json:"starts_at"`
			EndsAt               time.Time          `json:"ends_at"`
			Timezone             string             `json:"timezone"`
			ConfigVersion        string             `json:"config_version"`
			RandomVersion        string             `json:"random_version"`
			RuleCount            int                `json:"rule_count"`
			Rules                []PeriodRuleSpec   `json:"rules"`
			FundingPolicy        string             `json:"funding_policy"`
			TicketThresholdMilli int64              `json:"ticket_threshold_milli"`
			Rewards              []PeriodRewardSpec `json:"rewards"`
			RewardBudget         int64              `json:"reward_budget"`
		}{created.Key, string(status), created.StartsAt, created.EndsAt, created.Timezone, created.ConfigVersion, created.RandomVersion, len(rules), command.Rules, created.FundingPolicy, created.TicketThresholdMilli, command.Rewards, command.RewardBudget})
		if err != nil {
			return err
		}
		if err := repos.Audit.Append(ctx, ports.AuditLog{
			ActorType: command.ActorType, ActorID: command.ActorID, Action: "period_create",
			ResourceType: "period", ResourceID: created.Key, Reason: command.Reason,
			AfterJSON: afterJSON, RequestID: command.RequestID, CreatedAt: s.now(),
		}); err != nil {
			return err
		}
		result = PeriodCreateResult{
			PeriodID: created.ID, Key: created.Key, Status: string(status),
			StartsAt: created.StartsAt, EndsAt: created.EndsAt, RuleCount: len(rules),
			RewardCount: len(command.Rewards), RewardBudget: command.RewardBudget, FundingPolicy: created.FundingPolicy, TicketThresholdMilli: created.TicketThresholdMilli,
		}
		if command.RequestID != "" {
			response, err := json.Marshal(result)
			if err != nil {
				return err
			}
			status := 200
			replay.ResponseStatus, replay.ResponseJSON = &status, response
			replay.ResourceType, replay.ResourceID = "period", created.Key
			return repos.Idempotency.Save(ctx, replay)
		}
		return nil
	})
	if err != nil {
		return PeriodCreateResult{}, err
	}
	return result, nil
}

func normalizePeriodCreateCommand(command PeriodCreateCommand) (PeriodCreateCommand, error) {
	command.ActorType = strings.TrimSpace(command.ActorType)
	command.ActorID = strings.TrimSpace(command.ActorID)
	command.Key = strings.TrimSpace(command.Key)
	command.Timezone = strings.TrimSpace(command.Timezone)
	command.ConfigVersion = strings.TrimSpace(command.ConfigVersion)
	command.RandomVersion = strings.TrimSpace(command.RandomVersion)
	command.Reason = strings.TrimSpace(command.Reason)

	if command.ActorType == "" || command.ActorID == "" {
		return command, errors.New("period create actor is required")
	}
	// Invariant #16: every manual financial-shaped change must be auditable,
	// and an audit row without a reason is not.
	if command.Reason == "" {
		return command, errors.New("period create reason is required")
	}
	if command.Key == "" {
		return command, errors.New("period key is required")
	}
	if command.ConfigVersion == "" {
		return command, errors.New("period config version is required")
	}
	if command.RandomVersion == "" {
		return command, errors.New("period random version is required")
	}
	if command.Timezone == "" {
		command.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(command.Timezone); err != nil {
		return command, fmt.Errorf("invalid period timezone %q: %w", command.Timezone, err)
	}
	if command.StartsAt.IsZero() {
		return command, errors.New("period start time is required")
	}
	if len(command.Rules) == 0 {
		return command, errors.New("a period must define at least one economics rule")
	}
	if err := validatePeriodRewards(command.Rewards, command.RewardBudget, command.TicketThresholdMilli); err != nil {
		return command, err
	}
	seen := make(map[string]struct{}, len(command.Rules))
	for i, spec := range command.Rules {
		spec.Key = strings.TrimSpace(spec.Key)
		spec.ModelPattern = strings.TrimSpace(spec.ModelPattern)
		if spec.Key == "" {
			return command, errors.New("economics rule key is required")
		}
		if _, duplicate := seen[spec.Key]; duplicate {
			return command, fmt.Errorf("duplicate economics rule key %q", spec.Key)
		}
		seen[spec.Key] = struct{}{}
		if err := money.Bps(spec.MultiplierBps).Validate(); err != nil {
			return command, fmt.Errorf("economics rule %q: %w", spec.Key, err)
		}
		command.Rules[i] = spec
	}
	return command, nil
}

var periodRewardKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

const maxPublicRewardInteger uint64 = 1<<53 - 1

func validatePeriodRewards(rewards []PeriodRewardSpec, budget, threshold int64) error {
	if len(rewards) == 0 {
		if budget != 0 || threshold != 0 {
			return errors.New("reward budget and ticket threshold require a non-empty reward pool")
		}
		return nil
	}
	if len(rewards) > 50 || budget <= 0 || threshold <= 0 {
		return errors.New("a reward pool requires 1-50 prizes, a positive budget and a positive ticket threshold")
	}
	seen := make(map[string]bool, len(rewards))
	var total uint64
	for _, spec := range rewards {
		if !periodRewardKey.MatchString(spec.Key) || seen[spec.Key] || spec.Amount <= 0 || spec.Amount > int64(maxPublicRewardInteger) || spec.Amount > budget || spec.Weight == 0 || spec.Weight > maxPublicRewardInteger-total {
			return errors.New("invalid or duplicate prize: key must be canonical, amount must fit budget, amounts and total weight must be safe JSON integers")
		}
		seen[spec.Key] = true
		total += spec.Weight
	}
	return nil
}
