package service

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"math/big"
	"testing"
	"time"
)

type checkinPolicyPeriods struct{ admin *periodAdminMemory }

func (p checkinPolicyPeriods) FindActiveAt(_ context.Context, at time.Time) (period.Period, error) {
	return period.ResolveActive(p.admin.periods, at)
}
func (p checkinPolicyPeriods) FindActiveAtCurrent(ctx context.Context, at time.Time) (period.Period, error) {
	return p.FindActiveAt(ctx, at)
}

type checkinPolicyEconomics struct{ admin *economicsAdminMemory }

func (p checkinPolicyEconomics) ListRules(context.Context, uint64) ([]economics.Rule, error) {
	return p.admin.rules, nil
}

type checkinPolicyUnit struct {
	*periodCreateUnit
	budgets *memoryRewardStore
}

func (u checkinPolicyUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return u.periodCreateUnit.Do(ctx, func(r ports.Repositories) error {
		r.Period = checkinPolicyPeriods{u.admin}
		r.Economics = checkinPolicyEconomics{u.economics}
		r.Reward = u.budgets
		return fn(r)
	})
}
func policyFixture(t *testing.T, unlimited bool) (checkinPolicyUnit, *PeriodCreateService, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	raw := newPeriodCreateUnit()
	raw.admin.nextID = 1
	raw.admin.periods = []period.Period{{ID: 1, Key: "old", Status: period.StatusActive, Continuous: true, QuotaValidityDays: 60, StartsAt: now.Add(-time.Hour), EndsAt: now.AddDate(10, 0, 0), Timezone: "Asia/Shanghai", TicketThresholdMilli: 5000, FundingPolicy: period.VerifiedPaidFunding}}
	raw.economics.rules = []economics.Rule{{Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "old"}}
	u := checkinPolicyUnit{raw, newMemoryRewardStore()}
	for _, kind := range []string{ActionBudgetType, ExperienceRewardType} {
		cap := int64(1_000_000_000)
		if unlimited {
			cap = 0
		}
		u.budgets.budgets[budgetKey(1, kind)] = ports.RewardBudget{PeriodID: 1, BudgetType: kind, Unlimited: unlimited, HardCap: cap}
	}
	svc, err := NewPeriodCreateService(u, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return u, svc, now
}
func TestCheckinRewardPrizesExactExpectation(t *testing.T) {
	for _, unit := range []int64{3, 100000, 500000, int64(maxPublicRewardInteger) / 100} {
		rows, err := CheckinRewardPrizes(unit)
		if err != nil {
			t.Fatal(err)
		}
		total := uint64(0)
		weighted := new(big.Int)
		for _, r := range rows {
			total += r.Weight
			if r.RewardType == "newapi_quota" {
				weighted.Add(weighted, new(big.Int).Mul(big.NewInt(r.Amount), new(big.Int).SetUint64(r.Weight)))
			}
		}
		want := new(big.Int).Mul(big.NewInt(unit), big.NewInt(5*int64(total)))
		if total != 1000 || new(big.Int).Mul(weighted, big.NewInt(2)).Cmp(want) != 0 {
			t.Fatal(unit, total, weighted)
		}
	}
	for _, unit := range []int64{0, -1, int64(maxPublicRewardInteger)/100 + 1} {
		if _, err := CheckinRewardPrizes(unit); err == nil {
			t.Fatal("overflow accepted", unit)
		}
	}
}
func TestCheckinPolicyPublishesOncePreservingValidityBudgetsAndLaterRules(t *testing.T) {
	for _, unlimited := range []bool{false, true} {
		u, svc, now := policyFixture(t, unlimited)
		result, err := svc.ApplyCheckinRewardPolicy(context.Background(), 500000)
		if err != nil {
			t.Fatal(err)
		}
		if result.Key != CheckinRewardPolicyKey || len(u.admin.periods) != 2 || u.admin.periods[1].QuotaValidityDays != 60 || len(u.audit.logs) != 1 {
			t.Fatal(result, u.admin.periods, u.audit.logs)
		}
		if len(u.reward.definitions) != 9 {
			t.Fatal(u.reward.definitions)
		}
		for _, b := range u.reward.budgets {
			if b.Unlimited != unlimited || (!unlimited && b.HardCap != 1_000_000_000) {
				t.Fatal(b)
			}
		}
		old := u.admin.periods[0]
		if old.Key != "old" || old.ConfigVersion != "" {
			t.Fatal("old rule changed", old)
		}
		u.admin.periods = append(u.admin.periods, period.Period{ID: 99, Key: "manual-later", Continuous: true, Status: period.StatusActive, StartsAt: now, EndsAt: now.AddDate(10, 0, 0)})
		replay, err := svc.ApplyCheckinRewardPolicy(context.Background(), 100000)
		if err != nil || replay.PeriodID != result.PeriodID || len(u.admin.periods) != 3 || len(u.audit.logs) != 1 {
			t.Fatal(replay, err)
		}
	}
}
func TestCheckinPolicyFailureRollsBackNewSnapshot(t *testing.T) {
	u, svc, _ := policyFixture(t, true)
	u.audit.err = errors.New("audit unavailable")
	if _, err := svc.ApplyCheckinRewardPolicy(context.Background(), 500000); err == nil {
		t.Fatal("failure ignored")
	}
	if len(u.admin.periods) != 1 || len(u.reward.definitions) != 0 || len(u.reward.budgets) != 0 {
		t.Fatal("partial policy committed")
	}
}

func TestCheckinPolicyDoesNotInitializeAnUnconfiguredSite(t *testing.T) {
	u, svc, _ := policyFixture(t, true)
	u.admin.periods = nil
	result, err := svc.ApplyCheckinRewardPolicy(context.Background(), 500000)
	if err != nil || result.PeriodID != 0 || len(u.audit.logs) != 0 {
		t.Fatal(result, err)
	}
}
func TestCheckinPolicyRefusesUnpublishedPolicyKey(t *testing.T) {
	u, svc, _ := policyFixture(t, true)
	u.admin.periods = append(u.admin.periods, period.Period{ID: 2, Key: CheckinRewardPolicyKey, Status: period.StatusDraft})
	if _, err := svc.ApplyCheckinRewardPolicy(context.Background(), 500000); err == nil {
		t.Fatal("draft treated as applied")
	}
}

func TestDrawBonusPolicyCarriesActiveTiersAndDoesNotRepublishLaterRules(t *testing.T) {
	u, svc, now := policyFixture(t, true)
	u.admin.periods[0].ConfigVersion = "custom-live"
	u.admin.periods[0].QuotaValidityDays = 73
	u.budgets.definitions = []reward.Definition{
		{ID: 1, RewardKey: "quota-seven", RewardType: "newapi_quota", Amount: 700, Weight: 13, Enabled: true, ConfigVersion: "custom-live"},
		{ID: 2, RewardKey: "exp-four", RewardType: ExperienceRewardType, Amount: 4, Weight: 6, Enabled: true, ConfigVersion: "custom-live"},
	}
	result, err := svc.ApplyDrawBonusPolicy(context.Background(), 100)
	if err != nil || result.Key != DrawBonusPolicyKey {
		t.Fatalf("publish %+v %v", result, err)
	}
	if len(u.admin.periods) != 2 || u.admin.periods[1].QuotaValidityDays != 73 || len(u.reward.definitions) != 2 {
		t.Fatal("renewed ticket policy or missing configuration")
	}
	if d := u.reward.definitions[0]; d.Amount != 700 || d.Weight != 13 {
		t.Fatalf("quota tier reset: %+v", d)
	}
	if d := u.reward.definitions[1]; d.Amount != 4 || d.Weight != 6 {
		t.Fatalf("experience tier reset: %+v", d)
	}
	u.admin.periods = append(u.admin.periods, period.Period{ID: 99, Key: "later-manual", Continuous: true, Status: period.StatusActive, StartsAt: now, EndsAt: now.Add(time.Hour)})
	replay, err := svc.ApplyDrawBonusPolicy(context.Background(), 100)
	if err != nil || replay.PeriodID != result.PeriodID || len(u.admin.periods) != 3 {
		t.Fatalf("upgrade replaced later rule: %+v %v", replay, err)
	}
}
