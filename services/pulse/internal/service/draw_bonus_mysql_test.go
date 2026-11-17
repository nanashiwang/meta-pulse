package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
	id := uint64(now.UnixNano())
	var g ports.RewardGrant
	err = u.Do(ctx, func(r ports.Repositories) error {
		b, err := r.RewardAdmin.CreateBudget(ctx, ports.RewardBudget{PeriodID: id, BudgetType: ExperienceRewardType, HardCap: 20})
		if err != nil {
			return err
		}
		if err = reserveBudget(&b, 4); err != nil {
			return err
		}
		if err = r.Reward.SaveBudget(ctx, b); err != nil {
			return err
		}
		g, err = r.Reward.CreateGrant(ctx, ports.RewardGrant{GrantID: key, PeriodID: id, UserID: id, ActionID: key, TriggerType: ActionTriggerType, RewardDefinitionID: 1, RewardType: "newapi_quota", Amount: 100, Status: "pending", BudgetType: ActionBudgetType, ConfigVersion: DrawBonusPolicyKey, RandomValue: fmt.Sprintf("%064x", 1), SourceRef: key, Reason: "isolated integration fixture", CreatedAt: now, BonusExperience: 4, BonusExperienceStatus: "pending"})
		if err != nil {
			return err
		}
		payload := []byte(fmt.Sprintf(`{"grant_id":"%s","user_id":%d,"amount":100,"source_ref":"%s","reward_type":"newapi_quota","transferable_quota":false}`, key, id, key))
		_, err = r.Reward.CreateOutbox(ctx, ports.SettlementOutbox{RewardGrantID: g.ID, Operation: "grant", Status: "pending", PayloadJSON: payload, PayloadHash: canonicalJSONHash(payload), NextAttemptAt: now, CreatedAt: now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM pulse_settlement_outbox WHERE reward_grant_id=?", g.ID)
		db.Exec("DELETE FROM pulse_reward_grant WHERE id=?", g.ID)
		db.Exec("DELETE FROM pulse_reward_budget WHERE period_id=?", id)
	})
	deliveries := NewExperienceService(u)
	got, err := deliveries.Pending(ctx, id)
	if err != nil || len(got) != 1 || got[0] != (ExperienceDelivery{key, 4, "pending"}) {
		t.Fatalf("deliveries %+v %v", got, err)
	}
	if err = deliveries.Acknowledge(ctx, id+1, key, "pending"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("wrong user", err)
	}
	for i := 0; i < 2; i++ {
		if err = deliveries.Acknowledge(ctx, id, key, "pending"); err != nil {
			t.Fatal(err)
		}
	}
	if err = deliveries.Reverse(ctx, "admin", key, key, "isolated experience reversal"); err != nil {
		t.Fatal(err)
	}
	got, err = deliveries.Pending(ctx, id)
	if err != nil || len(got) != 1 || got[0] != (ExperienceDelivery{key, 4, "reversed"}) {
		t.Fatalf("reversal %+v %v", got, err)
	}
	for i := 0; i < 2; i++ {
		if err = deliveries.Acknowledge(ctx, id, key, "reversed"); err != nil {
			t.Fatal(err)
		}
	}
	err = u.Do(ctx, func(r ports.Repositories) error {
		stored, e := r.Reward.FindGrantByPublicID(ctx, key)
		if e != nil {
			return e
		}
		budget, e := r.Reward.GetBudget(ctx, id, ExperienceRewardType)
		if e != nil {
			return e
		}
		out, e := r.Settlement.FindByGrant(ctx, g.ID)
		if e != nil {
			return e
		}
		if stored.Status != "pending" || stored.BonusExperienceStatus != "reversed" || out.Status != "pending" || budget.ReservedAmount != 0 || budget.SettledAmount != 0 || budget.ReleasedAmount != 4 {
			t.Fatalf("flash/EXP isolation: %+v %+v %+v", stored, out, budget)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
