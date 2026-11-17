package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

func TestMySQLDrawBonusDeliveryKeepsQuotaOutboxSeparate(t *testing.T) {
	database, db := openMySQLIntegration(t)
	u, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	key := fmt.Sprintf("draw-bonus-sql-%d", now.UnixNano())
	userID := uint64(now.UnixNano()%1_000_000_000) + 2_000_000_000
	var periodID uint64
	var g ports.RewardGrant
	err = u.Do(ctx, func(r ports.Repositories) error {
		// Leave the delivery fixture in draft so it does not replace any active
		// integration period. Definitions and budgets still use real store guards.
		p, err := r.PeriodAdmin.Create(ctx, period.Period{
			Key: key, Status: period.StatusDraft, StartsAt: now, EndsAt: now.Add(time.Hour), Timezone: "Asia/Shanghai",
			ConfigVersion: DrawBonusPolicyKey, RandomVersion: DrawBonusPolicyKey,
			FundingPolicy: period.VerifiedPaidFunding, TicketThresholdMilli: 1000,
		})
		if err != nil {
			return err
		}
		periodID = p.ID
		primary, err := r.RewardAdmin.CreateDefinition(ctx, periodID, reward.Definition{
			RewardKey: "flash", RewardType: "newapi_quota", Amount: 100, Weight: 1, Enabled: true, ConfigVersion: DrawBonusPolicyKey,
		})
		if err != nil {
			return err
		}
		if _, err = r.RewardAdmin.CreateDefinition(ctx, periodID, reward.Definition{
			RewardKey: "bonus-exp", RewardType: ExperienceRewardType, Amount: 4, Weight: 1, Enabled: true, ConfigVersion: DrawBonusPolicyKey,
		}); err != nil {
			return err
		}
		for _, fixture := range []struct {
			kind          string
			cap, reserved int64
		}{{ActionBudgetType, 1000, 100}, {ExperienceRewardType, 20, 4}} {
			b, err := r.RewardAdmin.CreateBudget(ctx, ports.RewardBudget{PeriodID: periodID, BudgetType: fixture.kind, HardCap: fixture.cap})
			if err != nil {
				return err
			}
			if err = reserveBudget(&b, fixture.reserved); err != nil {
				return err
			}
			if err = r.Reward.SaveBudget(ctx, b); err != nil {
				return err
			}
		}
		g, err = r.Reward.CreateGrant(ctx, ports.RewardGrant{GrantID: key, PeriodID: periodID, UserID: userID, ActionID: key, TriggerType: ActionTriggerType, RewardDefinitionID: primary.ID, RewardType: "newapi_quota", Amount: 100, Status: "pending", BudgetType: ActionBudgetType, ConfigVersion: DrawBonusPolicyKey, RandomValue: fmt.Sprintf("%064x", 1), SourceRef: key, Reason: "isolated integration fixture", CreatedAt: now, BonusExperience: 4, BonusExperienceStatus: "pending"})
		if err != nil {
			return err
		}
		payload := []byte(fmt.Sprintf(`{"grant_id":"%s","user_id":%d,"amount":100,"source_ref":"%s","reward_type":"newapi_quota","transferable_quota":false}`, key, userID, key))
		_, err = r.Reward.CreateOutbox(ctx, ports.SettlementOutbox{RewardGrantID: g.ID, Operation: "grant", Status: "pending", PayloadJSON: payload, PayloadHash: canonicalJSONHash(payload), NextAttemptAt: now, CreatedAt: now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM pulse_settlement_outbox WHERE reward_grant_id=?", g.ID)
		db.Exec("DELETE FROM pulse_reward_grant WHERE id=?", g.ID)
		db.Exec("DELETE FROM pulse_reward_budget WHERE period_id=?", periodID)
		db.Exec("DELETE FROM pulse_reward_definition WHERE period_id=?", periodID)
		db.Exec("DELETE FROM pulse_period WHERE id=?", periodID)
	})
	deliveries := NewExperienceService(u)
	got, err := deliveries.Pending(ctx, userID)
	if err != nil || len(got) != 1 || got[0] != (ExperienceDelivery{key, 4, "pending"}) {
		t.Fatalf("deliveries %+v %v", got, err)
	}
	if err = deliveries.Acknowledge(ctx, userID+1, key, "pending"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("wrong user", err)
	}
	for i := 0; i < 2; i++ {
		if err = deliveries.Acknowledge(ctx, userID, key, "pending"); err != nil {
			t.Fatal(err)
		}
	}
	if err = deliveries.Reverse(ctx, "admin", key, key, "isolated experience reversal"); err != nil {
		t.Fatal(err)
	}
	got, err = deliveries.Pending(ctx, userID)
	if err != nil || len(got) != 1 || got[0] != (ExperienceDelivery{key, 4, "reversed"}) {
		t.Fatalf("reversal %+v %v", got, err)
	}
	for i := 0; i < 2; i++ {
		if err = deliveries.Acknowledge(ctx, userID, key, "reversed"); err != nil {
			t.Fatal(err)
		}
	}
	err = u.Do(ctx, func(r ports.Repositories) error {
		stored, e := r.Reward.FindGrantByPublicID(ctx, key)
		if e != nil {
			return e
		}
		budget, e := r.Reward.GetBudget(ctx, periodID, ExperienceRewardType)
		if e != nil {
			return e
		}
		quota, e := r.Reward.GetBudget(ctx, periodID, ActionBudgetType)
		if e != nil {
			return e
		}
		out, e := r.Settlement.FindByGrant(ctx, g.ID)
		if e != nil {
			return e
		}
		if stored.Status != "pending" || stored.BonusExperienceStatus != "reversed" || out.Status != "pending" || budget.ReservedAmount != 0 || budget.SettledAmount != 0 || budget.ReleasedAmount != 4 || quota.ReservedAmount != g.Amount || quota.SettledAmount != 0 || quota.ReleasedAmount != 0 {
			t.Fatalf("flash/EXP isolation: grant=%+v outbox=%+v exp=%+v quota=%+v", stored, out, budget, quota)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
