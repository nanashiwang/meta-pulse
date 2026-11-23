package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type CheckinFlash struct {
	Day     string `json:"day"`
	Cents   int64  `json:"cents"`
	EventID uint64 `json:"event_id"`
}
type CheckinFlashResult struct {
	GrantID string `json:"grant_id"`
	Cents   int64  `json:"cents"`
	Status  string `json:"status"`
}

const checkinFlashPrefix = "checkin-flash-v1-"

// The signed Forum server must persist the random cents before calling this.
// Nothing is sent to the benefit receiver until this transaction commits.
func (s *CommunityTicketService) GrantCheckinFlash(ctx context.Context, user uint64, task CheckinFlash) (CheckinFlashResult, error) {
	day, err := time.ParseInLocation("2006-01-02", task.Day, communityZone)
	if err != nil || day.Format("2006-01-02") != task.Day || task.Day < "2026-11-18" || day.After(s.now()) || user == 0 || task.EventID == 0 || task.Cents < 1 || task.Cents > 150 {
		return CheckinFlashResult{}, ports.ErrConflict
	}
	key := fmt.Sprintf("daily-checkin-v1:%d:%s", user, task.Day)
	return s.grantForumFlash(ctx, user, task, key, false)
}

// GrantGiftFlash is worker-only; values come from a committed round receipt,
// never from a user-supplied prize or from a mutable draw rule.
func (s *CommunityTicketService) GrantGiftFlash(ctx context.Context, user, round uint64, cents int64) (CheckinFlashResult, error) {
	if user == 0 || round == 0 || cents < 1 || cents > 1000000 {
		return CheckinFlashResult{}, ports.ErrConflict
	}
	return s.grantForumFlash(ctx, user, CheckinFlash{EventID: round, Cents: cents}, fmt.Sprintf("limited-gift:%d:%d", round, user), true)
}
func (s *CommunityTicketService) grantForumFlash(ctx context.Context, user uint64, task CheckinFlash, key string, gift bool) (CheckinFlashResult, error) {
	scope, reason, trigger, maxCents := "checkin_flash", "每日签到随机闪", "checkin", int64(150)
	if gift {
		scope, reason, trigger, maxCents = "limited_gift", "限时赠礼", "gift", task.Cents
		// Keep the same idempotency namespace across worker versions. The upper
		// receipt range is reserved for published forum content awards.
		if task.EventID >= 1<<62 {
			reason = "优秀内容评选"
		}
	}
	raw, _ := json.Marshal(task)
	hash := sha256Hex(raw)
	var out CheckinFlashResult
	err := s.unit.Do(ctx, func(r ports.Repositories) error {
		if err := lockRewardRule(ctx, r.Idempotency); err != nil {
			return err
		}
		idem, err := r.Idempotency.GetOrCreateForUpdate(ctx, scope, key, hash)
		if err != nil {
			return err
		}
		if idem.PayloadHash != hash {
			return ports.ErrConflict
		}
		if len(idem.ResponseJSON) > 0 {
			return json.Unmarshal(idem.ResponseJSON, &out)
		}
		if !s.CheckinFlashEnabled {
			return errors.New("check-in flash issuance paused")
		}
		unit := s.QuotaPerUnit
		if unit <= 0 || unit%100 != 0 || unit/100 > int64(maxPublicRewardInteger)/maxCents {
			return errors.New("quota unit must support exact cent-flash conversion")
		}
		now := s.now()
		policyKey := fmt.Sprintf("%s%d", checkinFlashPrefix, unit)
		if gift {
			policyKey = fmt.Sprintf("%sgift-%d", checkinFlashPrefix, unit)
		}
		p, err := r.PeriodAdmin.FindByKeyForUpdate(ctx, policyKey)
		if errors.Is(err, ports.ErrNotFound) {
			p, err = r.PeriodAdmin.Create(ctx, period.Period{Key: policyKey, Status: period.StatusDraft, StartsAt: time.Date(2026, 11, 18, 0, 0, 0, 0, communityZone), EndsAt: time.Date(9999, 1, 1, 0, 0, 0, 0, communityZone), Timezone: "Asia/Shanghai", ConfigVersion: policyKey, RandomVersion: "forum-crypto-rand-v1", FundingPolicy: period.VerifiedPaidFunding, TicketThresholdMilli: 1, Continuous: true, QuotaValidityDays: 1})
			if err != nil {
				return err
			}
			first, last := int64(1), int64(150)
			if gift {
				first, last = task.Cents, task.Cents
			}
			for cents := first; cents <= last; cents++ {
				_, err = r.RewardAdmin.CreateDefinition(ctx, p.ID, reward.Definition{RewardKey: fmt.Sprintf("checkin-%03d", cents), RewardType: "newapi_quota", Amount: (unit / 100) * cents, Weight: 1, ConfigVersion: policyKey, Enabled: true})
				if err != nil {
					return err
				}
			}
			_, err = r.RewardAdmin.CreateBudget(ctx, ports.RewardBudget{PeriodID: p.ID, BudgetType: ActionBudgetType, Unlimited: true})
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if p.Status != period.StatusDraft || p.ConfigVersion != policyKey {
			return ports.ErrConflict
		}
		defs, err := r.Reward.ListDefinitions(ctx, p.ID)
		if err != nil {
			return err
		}
		var def reward.Definition
		for _, d := range defs {
			if d.RewardKey == fmt.Sprintf("checkin-%03d", task.Cents) {
				def = d
				break
			}
		}
		amount := (unit / 100) * task.Cents
		if gift && def.ID == 0 {
			def, err = r.RewardAdmin.CreateDefinition(ctx, p.ID, reward.Definition{RewardKey: fmt.Sprintf("checkin-%03d", task.Cents), RewardType: "newapi_quota", Amount: amount, Weight: 1, ConfigVersion: policyKey, Enabled: true})
			if err != nil {
				return err
			}
		}
		if def.ID == 0 || !def.Enabled || def.Amount != amount || def.RewardType != "newapi_quota" {
			return ports.ErrConflict
		}
		budget, err := r.Reward.GetBudgetForUpdate(ctx, p.ID, ActionBudgetType)
		if err != nil {
			return err
		}
		if err = reserveBudget(&budget, amount); err != nil {
			return err
		}
		id := reward.GrantID(p.ID, user, key)
		g, err := r.Reward.CreateGrant(ctx, ports.RewardGrant{GrantID: id, PeriodID: p.ID, UserID: user, ActionID: key, TriggerType: trigger, RewardDefinitionID: def.ID, RewardType: "newapi_quota", Amount: amount, BudgetType: ActionBudgetType, RandomValue: hash, ConfigVersion: p.ConfigVersion, Status: RewardStatusPending, SourceRef: id, Reason: reason, CreatedAt: now})
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"grant_id": id, "user_id": user, "amount": amount, "transferable_quota": false, "source_ref": id, "reward_type": "newapi_quota"})
		_, err = r.Reward.CreateOutbox(ctx, ports.SettlementOutbox{RewardGrantID: g.ID, Operation: "grant", PayloadHash: canonicalJSONHash(payload), PayloadJSON: payload, Status: OutboxStatusPending, NextAttemptAt: now, CreatedAt: now})
		if err != nil {
			return err
		}
		if err = r.Reward.SaveBudget(ctx, budget); err != nil {
			return err
		}
		if err = r.Audit.Append(ctx, ports.AuditLog{ActorType: "community-bff", ActorID: fmt.Sprint(user), Action: scope, ResourceType: "reward_grant", ResourceID: id, Reason: reason, AfterJSON: raw, RequestID: key, CreatedAt: now}); err != nil {
			return err
		}
		out = CheckinFlashResult{GrantID: id, Cents: task.Cents, Status: "pending"}
		idem.ResponseJSON, _ = json.Marshal(out)
		status := 200
		idem.ResponseStatus = &status
		idem.ResourceType = "reward_grant"
		idem.ResourceID = id
		return r.Idempotency.Save(ctx, idem)
	})
	return out, err
}
func (s *CommunityTicketService) CheckinFlashStatus(ctx context.Context, user uint64, id string) (CheckinFlashResult, error) {
	var out CheckinFlashResult
	if user == 0 || !validDBText(id, 64) {
		return out, ports.ErrConflict
	}
	err := s.unit.Do(ctx, func(r ports.Repositories) error {
		g, err := r.Reward.FindGrantByPublicID(ctx, id)
		if err != nil {
			return err
		}
		if g.UserID != user || g.TriggerType != "checkin" {
			return ports.ErrConflict
		}
		defs, err := r.Reward.ListDefinitions(ctx, g.PeriodID)
		if err != nil {
			return err
		}
		var cents int64
		for _, d := range defs {
			if d.ID == g.RewardDefinitionID {
				if _, err = fmt.Sscanf(d.RewardKey, "checkin-%d", &cents); err != nil {
					return err
				}
				break
			}
		}
		if cents < 1 || cents > 150 {
			return ports.ErrConflict
		}
		state := g.Status
		if state == "pending" {
			box, err := r.Settlement.FindByGrant(ctx, g.ID)
			if err != nil {
				return err
			}
			if box.Status == OutboxStatusDead || box.Status == OutboxStatusConflict {
				state = "blocked"
			}
		}
		out = CheckinFlashResult{GrantID: id, Cents: cents, Status: state}
		return nil
	})
	return out, err
}
