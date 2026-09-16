package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
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
	ActorType     string
	ActorID       string
	Key           string
	StartsAt      time.Time
	Timezone      string
	ConfigVersion string
	RandomVersion string
	Rules         []PeriodRuleSpec
	Activate      bool
	Reason        string
}

type PeriodCreateResult struct {
	PeriodID  uint64    `json:"period_id"`
	Key       string    `json:"period_key"`
	Status    string    `json:"status"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	RuleCount int       `json:"rule_count"`
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
		return PeriodCreateResult{}, err
	}
	endsAt := command.StartsAt.Add(PeriodLength)

	var result PeriodCreateResult
	err = s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.PeriodAdmin == nil || repos.EconomicsAdmin == nil || repos.Audit == nil {
			return errors.New("period create repositories are not initialized")
		}
		overlapping, err := repos.PeriodAdmin.ListOverlapping(ctx, command.StartsAt, endsAt)
		if err != nil {
			return err
		}
		if len(overlapping) > 0 {
			return fmt.Errorf("%w: period %s overlaps existing period %s", ports.ErrConflict, command.Key, overlapping[0].Key)
		}
		created, err := repos.PeriodAdmin.Create(ctx, period.Period{
			Key: command.Key, Status: period.StatusDraft,
			StartsAt: command.StartsAt, EndsAt: endsAt, Timezone: command.Timezone,
			ConfigVersion: command.ConfigVersion, RandomVersion: command.RandomVersion,
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
		status := period.StatusDraft
		if command.Activate {
			if err := repos.PeriodAdmin.Transition(ctx, created.ID, period.StatusDraft, period.StatusActive, s.now()); err != nil {
				return err
			}
			status = period.StatusActive
		}
		afterJSON, err := json.Marshal(struct {
			PeriodKey     string    `json:"period_key"`
			Status        string    `json:"status"`
			StartsAt      time.Time `json:"starts_at"`
			EndsAt        time.Time `json:"ends_at"`
			Timezone      string    `json:"timezone"`
			ConfigVersion string    `json:"config_version"`
			RandomVersion string    `json:"random_version"`
			RuleCount     int       `json:"rule_count"`
		}{created.Key, string(status), created.StartsAt, created.EndsAt, created.Timezone, created.ConfigVersion, created.RandomVersion, len(rules)})
		if err != nil {
			return err
		}
		if err := repos.Audit.Append(ctx, ports.AuditLog{
			ActorType: command.ActorType, ActorID: command.ActorID, Action: "period_create",
			ResourceType: "period", ResourceID: created.Key, Reason: command.Reason,
			AfterJSON: afterJSON, CreatedAt: s.now(),
		}); err != nil {
			return err
		}
		result = PeriodCreateResult{
			PeriodID: created.ID, Key: created.Key, Status: string(status),
			StartsAt: created.StartsAt, EndsAt: created.EndsAt, RuleCount: len(rules),
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
