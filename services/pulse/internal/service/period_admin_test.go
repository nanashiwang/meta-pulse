package service

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"strings"
	"testing"
	"time"
)

func validAdminPeriod() PeriodAdminRequest {
	return PeriodAdminRequest{Key: "web-period", StartsAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), MultiplierBps: 12500, TicketThresholdMilli: 1500250, RewardBudget: 1000, Rewards: []PeriodRewardSpec{{Key: "prize", Amount: 100, Weight: 1}}, Reason: "reviewed economics"}
}
func TestAdminPeriodReplayAndPayloadConflict(t *testing.T) {
	unit := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(unit, time.Now)
	request := validAdminPeriod()
	for i := 0; i < 100; i++ {
		result, err := svc.CreateFromAdmin(context.Background(), request, "42", "request-1")
		if err != nil || result.Status != "active" || result.TicketThresholdMilli != 1500250 {
			t.Fatalf("replay %d: %+v %v", i, result, err)
		}
	}
	if len(unit.admin.periods) != 1 || len(unit.audit.logs) != 1 || len(unit.reward.budgets) != 1 || unit.economics.rules[0].MultiplierBps != 12500 || unit.audit.logs[0].RequestID != "request-1" {
		t.Fatal("duplicate writes or lost economics/audit")
	}
	request.MultiplierBps = 20000
	if _, err := svc.CreateFromAdmin(context.Background(), request, "42", "request-1"); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
	if unit.economics.rules[0].MultiplierBps != 12500 {
		t.Fatal("active rule changed")
	}
}
func TestAdminPeriodValidation(t *testing.T) {
	svc, _ := NewPeriodCreateService(newPeriodCreateUnit(), time.Now)
	for _, change := range []func(*PeriodAdminRequest){
		func(r *PeriodAdminRequest) { r.MultiplierBps = 0 }, func(r *PeriodAdminRequest) { r.MultiplierBps = 1000001 },
		func(r *PeriodAdminRequest) { r.TicketThresholdMilli = 0 }, func(r *PeriodAdminRequest) { r.TicketThresholdMilli = 1 << 53 },
		func(r *PeriodAdminRequest) { r.RewardBudget = 1 << 53 }, func(r *PeriodAdminRequest) { r.Rewards = nil },
		func(r *PeriodAdminRequest) { r.Rewards[0].Amount = r.RewardBudget + 1 }, func(r *PeriodAdminRequest) { r.Reason = "" }, func(r *PeriodAdminRequest) { r.Key = "bad/key" },
	} {
		r := validAdminPeriod()
		change(&r)
		if _, err := svc.CreateFromAdmin(context.Background(), r, "42", "request-1"); !errors.Is(err, ErrInvalidPeriod) {
			t.Fatalf("bad request accepted %+v %v", r, err)
		}
	}
}

func TestAdminPeriodDuplicateKeyAndMaximumKeyLength(t *testing.T) {
	svc, _ := NewPeriodCreateService(newPeriodCreateUnit(), time.Now)
	request := validAdminPeriod()
	request.Key = strings.Repeat("p", 64)
	if _, err := svc.CreateFromAdmin(context.Background(), request, "42", "request-1"); err != nil {
		t.Fatal(err)
	}
	request.StartsAt = request.StartsAt.Add(PeriodLength)
	if _, err := svc.CreateFromAdmin(context.Background(), request, "42", "request-2"); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("duplicate key accepted: %v", err)
	}
}
