package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type economicsAdminMemory struct {
	rules   []economics.Rule
	nextID  uint64
	failAll bool
}

func (m *economicsAdminMemory) CreateRule(_ context.Context, periodID uint64, rule economics.Rule) (economics.Rule, error) {
	if m.failAll {
		return economics.Rule{}, errors.New("simulated rule write failure")
	}
	m.nextID++
	rule.ID = m.nextID
	m.rules = append(m.rules, rule)
	_ = periodID
	return rule, nil
}

type auditMemory struct{ logs []ports.AuditLog }

func (m *auditMemory) Append(_ context.Context, log ports.AuditLog) error {
	m.logs = append(m.logs, log)
	return nil
}

type periodCreateUnit struct {
	admin     *periodAdminMemory
	economics *economicsAdminMemory
	audit     *auditMemory
	cursor    ports.CursorRepository
	// rollback mirrors the real transaction: a failed callback discards every
	// write the callback made.
	rollback bool
}

func (u *periodCreateUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	adminSnapshot := append([]period.Period(nil), u.admin.periods...)
	rulesSnapshot := append([]economics.Rule(nil), u.economics.rules...)
	auditSnapshot := append([]ports.AuditLog(nil), u.audit.logs...)
	err := fn(ports.Repositories{
		PeriodAdmin: u.admin, EconomicsAdmin: u.economics, Audit: u.audit, Cursor: u.cursor,
	})
	if err != nil && u.rollback {
		u.admin.periods = adminSnapshot
		u.economics.rules = rulesSnapshot
		u.audit.logs = auditSnapshot
	}
	return err
}

func newPeriodCreateUnit() *periodCreateUnit {
	return &periodCreateUnit{
		admin: &periodAdminMemory{}, economics: &economicsAdminMemory{},
		audit: &auditMemory{}, rollback: true,
	}
}

func validCreateCommand() PeriodCreateCommand {
	return PeriodCreateCommand{
		ActorType: "operator", ActorID: "ops-1", Key: "2026-P001",
		StartsAt:      time.Date(2026, 9, 16, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		ConfigVersion: "v1", RandomVersion: "v1", Reason: "首个生产周期",
		Rules: []PeriodRuleSpec{{Key: "default", Eligible: true, MultiplierBps: 10000}},
	}
}

func TestCreateWritesPeriodRulesAndAudit(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, err := NewPeriodCreateService(unit, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Create(context.Background(), validCreateCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleCount != 1 || len(unit.economics.rules) != 1 {
		t.Fatalf("rule count = %d, stored = %d, want 1/1", result.RuleCount, len(unit.economics.rules))
	}
	if len(unit.audit.logs) != 1 || unit.audit.logs[0].Action != "period_create" {
		t.Fatalf("audit logs = %+v", unit.audit.logs)
	}
	if unit.audit.logs[0].Reason == "" {
		t.Fatal("audit log has no reason")
	}
	// The fixed 10-day window comes from the architecture baseline, not the caller.
	if got := result.EndsAt.Sub(result.StartsAt); got != PeriodLength {
		t.Fatalf("period length = %v, want %v", got, PeriodLength)
	}
}

// Without --activate the period must stay a draft: FindActiveAt only matches
// active periods, so a draft cannot absorb usage events yet.
func TestCreateLeavesPeriodDraftUnlessActivated(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(unit, time.Now)
	result, err := svc.Create(context.Background(), validCreateCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != string(period.StatusDraft) {
		t.Fatalf("status = %s, want draft", result.Status)
	}
	if unit.admin.periods[0].Status != period.StatusDraft {
		t.Fatalf("stored status = %s, want draft", unit.admin.periods[0].Status)
	}
}

func TestCreateActivatesWhenRequested(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(unit, time.Now)
	command := validCreateCommand()
	command.Activate = true
	result, err := svc.Create(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != string(period.StatusActive) {
		t.Fatalf("status = %s, want active", result.Status)
	}
	if got := unit.admin.transitions; len(got) != 1 || got[0] != "draft->active" {
		t.Fatalf("transitions = %v, want one draft->active", got)
	}
}

// Overlapping periods would make ResolveActive ambiguous and stall ingestion
// with "overlapping active periods" on every batch.
func TestCreateRejectsOverlappingPeriod(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(unit, time.Now)
	first := validCreateCommand()
	if _, err := svc.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := validCreateCommand()
	second.Key = "2026-P002"
	second.StartsAt = first.StartsAt.Add(24 * time.Hour)
	_, err := svc.Create(context.Background(), second)
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("error = %v, want a conflict", err)
	}
}

// A period whose window starts exactly where another ends is adjacent, not
// overlapping: period.Contains uses a half-open interval.
func TestCreateAllowsAdjacentPeriod(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(unit, time.Now)
	first := validCreateCommand()
	if _, err := svc.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := validCreateCommand()
	second.Key = "2026-P002"
	second.StartsAt = first.StartsAt.Add(PeriodLength)
	if _, err := svc.Create(context.Background(), second); err != nil {
		t.Fatalf("adjacent period rejected: %v", err)
	}
}

// A period with no rule records every event as ineligible with zero
// contribution, and invariant #11 forbids fixing it in place afterwards.
func TestCreateRequiresAtLeastOneRule(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(unit, time.Now)
	command := validCreateCommand()
	command.Rules = nil
	if _, err := svc.Create(context.Background(), command); err == nil {
		t.Fatal("period without rules accepted")
	}
}

func TestCreateRequiresAuditableReasonAndActor(t *testing.T) {
	svc, _ := NewPeriodCreateService(newPeriodCreateUnit(), time.Now)
	for name, mutate := range map[string]func(*PeriodCreateCommand){
		"no reason":     func(c *PeriodCreateCommand) { c.Reason = "  " },
		"no actor id":   func(c *PeriodCreateCommand) { c.ActorID = "" },
		"no actor type": func(c *PeriodCreateCommand) { c.ActorType = "" },
	} {
		command := validCreateCommand()
		mutate(&command)
		if _, err := svc.Create(context.Background(), command); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestCreateRejectsOutOfRangeMultiplier(t *testing.T) {
	svc, _ := NewPeriodCreateService(newPeriodCreateUnit(), time.Now)
	command := validCreateCommand()
	command.Rules[0].MultiplierBps = 1000001
	if _, err := svc.Create(context.Background(), command); err == nil {
		t.Fatal("out-of-range multiplier accepted")
	}
}

func TestCreateRejectsDuplicateRuleKeys(t *testing.T) {
	svc, _ := NewPeriodCreateService(newPeriodCreateUnit(), time.Now)
	command := validCreateCommand()
	command.Rules = append(command.Rules, PeriodRuleSpec{Key: "default", Eligible: true, MultiplierBps: 20000})
	if _, err := svc.Create(context.Background(), command); err == nil {
		t.Fatal("duplicate rule key accepted")
	}
}

// A rule write that fails after the period row exists must not leave a period
// behind, or the operator's retry would hit a duplicate key.
func TestCreateRollsBackPeriodWhenRuleWriteFails(t *testing.T) {
	unit := newPeriodCreateUnit()
	unit.economics.failAll = true
	svc, _ := NewPeriodCreateService(unit, time.Now)
	if _, err := svc.Create(context.Background(), validCreateCommand()); err == nil {
		t.Fatal("rule failure was reported as success")
	}
	if len(unit.admin.periods) != 0 {
		t.Fatalf("period survived a failed rule write: %+v", unit.admin.periods)
	}
	if len(unit.audit.logs) != 0 {
		t.Fatal("audit log written for a rolled-back period")
	}
}

func TestCreateRejectsInvalidTimezone(t *testing.T) {
	svc, _ := NewPeriodCreateService(newPeriodCreateUnit(), time.Now)
	command := validCreateCommand()
	command.Timezone = "Mars/Olympus"
	_, err := svc.Create(context.Background(), command)
	if err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("error = %v, want a timezone error", err)
	}
}
