package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"math"
	"strings"
	"time"
)

// ExperienceService delivers non-monetary prizes to the authenticated Answer
// BFF. Answer commits its ledger before acknowledging; lost responses replay
// the same grant. No delivery cursor can skip a late transaction commit.
type ExperienceService struct {
	unit ports.UnitOfWork
	now  func() time.Time
}
type ExperienceDelivery struct {
	GrantID string `json:"grant_id"`
	Amount  int64  `json:"amount"`
	Status  string `json:"status"`
}

func NewExperienceService(unit ports.UnitOfWork) *ExperienceService {
	return &ExperienceService{unit: unit, now: time.Now}
}
func (s *ExperienceService) Pending(ctx context.Context, user uint64) ([]ExperienceDelivery, error) {
	out := []ExperienceDelivery{}
	if user == 0 {
		return nil, ports.ErrConflict
	}
	err := s.unit.Do(ctx, func(r ports.Repositories) error {
		if r.Experience == nil {
			return errors.New("experience delivery unavailable")
		}
		rows, err := r.Experience.ListPendingExperience(ctx, user, 20)
		if err != nil {
			return err
		}
		for _, g := range rows {
			if g.UserID != user || g.RewardType != ExperienceRewardType || g.Amount < 1 || g.Amount > 1000000 || (g.Status != "pending" && g.Status != "reversed") {
				return ports.ErrConflict
			}
			out = append(out, ExperienceDelivery{g.GrantID, g.Amount, g.Status})
		}
		return nil
	})
	return out, err
}
func (s *ExperienceService) Acknowledge(ctx context.Context, user uint64, id, state string) error {
	if user == 0 || !validDBText(id, 64) || (state != "pending" && state != "reversed") {
		return ports.ErrConflict
	}
	return s.unit.Do(ctx, func(r ports.Repositories) error {
		if r.Reward == nil || r.Experience == nil || r.Settlement == nil {
			return errors.New("experience delivery unavailable")
		}
		found, err := r.Reward.FindGrantByPublicID(ctx, id)
		if err != nil {
			return err
		}
		g, err := r.Reward.FindGrantByIDForUpdate(ctx, found.ID)
		if err != nil {
			return err
		}
		if g.UserID != user || g.RewardType != ExperienceRewardType || g.BudgetType != ExperienceRewardType {
			return ports.ErrConflict
		}
		deliveryStatus, err := r.Experience.ExperienceDeliveryStatusForUpdate(ctx, g.ID)
		if err != nil {
			return err
		}
		expected := state
		if state == "pending" {
			expected = "settled"
		}
		if deliveryStatus == "community_delivered" && g.Status == expected {
			return nil
		}
		if deliveryStatus != "community_pending" || g.Status != state {
			return ports.ErrConflict
		}
		if state == "pending" {
			b, err := r.Reward.GetBudgetForUpdate(ctx, g.PeriodID, g.BudgetType)
			if err != nil {
				return err
			}
			if b.ReservedAmount < g.Amount || b.SettledAmount > math.MaxInt64-g.Amount || b.Version == math.MaxUint64 {
				return ErrBudgetExceeded
			}
			b.ReservedAmount -= g.Amount
			b.SettledAmount += g.Amount
			b.Version++
			if err = r.Reward.SaveBudget(ctx, b); err != nil {
				return err
			}
			if err = r.Reward.TransitionGrantStatus(ctx, g.ID, "pending", "settled", s.now()); err != nil {
				return err
			}
		}
		return r.Experience.TransitionExperienceDelivery(ctx, g.ID, "community_pending", "community_delivered", s.now())
	})
}
func (s *ExperienceService) Reverse(ctx context.Context, actor, key, id, reason string) error {
	if !validDBText(id, 64) || !validDBText(actor, 64) || !validDBText(key, 96) || !validDBText(strings.TrimSpace(reason), 255) {
		return ports.ErrConflict
	}
	raw, _ := json.Marshal([]string{id, reason})
	hash := sha256Hex(raw)
	return s.unit.Do(ctx, func(r ports.Repositories) error {
		if r.Reward == nil || r.Experience == nil || r.Settlement == nil || r.Idempotency == nil || r.Audit == nil {
			return errors.New("experience delivery unavailable")
		}
		idem, err := r.Idempotency.GetOrCreateForUpdate(ctx, "experience_reverse:"+actor, key, hash)
		if err != nil {
			return err
		}
		if idem.PayloadHash != hash {
			return ports.ErrConflict
		}
		if len(idem.ResponseJSON) > 0 {
			return nil
		}
		found, err := r.Reward.FindGrantByPublicID(ctx, id)
		if err != nil {
			return err
		}
		g, err := r.Reward.FindGrantByIDForUpdate(ctx, found.ID)
		if err != nil {
			return err
		}
		if g.RewardType != ExperienceRewardType || g.BudgetType != ExperienceRewardType {
			return ports.ErrConflict
		}
		if g.Status != "reversed" {
			if g.Status != "pending" && g.Status != "settled" {
				return ports.ErrConflict
			}
			b, err := r.Reward.GetBudgetForUpdate(ctx, g.PeriodID, g.BudgetType)
			if err != nil {
				return err
			}
			if b.Version == math.MaxUint64 || b.ReleasedAmount > math.MaxInt64-g.Amount {
				return ErrBudgetExceeded
			}
			if g.Status == "pending" {
				if b.ReservedAmount < g.Amount {
					return ErrBudgetExceeded
				}
				b.ReservedAmount -= g.Amount
			} else {
				if b.SettledAmount < g.Amount {
					return ErrBudgetExceeded
				}
				b.SettledAmount -= g.Amount
			}
			b.ReleasedAmount += g.Amount
			b.Version++
			if err = r.Reward.SaveBudget(ctx, b); err != nil {
				return err
			}
			if err = r.Reward.TransitionGrantStatus(ctx, g.ID, g.Status, "reversed", s.now()); err != nil {
				return err
			}
			deliveryStatus, err := r.Experience.ExperienceDeliveryStatusForUpdate(ctx, g.ID)
			if err != nil {
				return err
			}
			if deliveryStatus != "community_pending" {
				if err = r.Experience.TransitionExperienceDelivery(ctx, g.ID, deliveryStatus, "community_pending", s.now()); err != nil {
					return err
				}
			}
		}
		if err = r.Audit.Append(ctx, ports.AuditLog{ActorType: "admin", ActorID: actor, Action: "experience_reverse", ResourceType: "reward_grant", ResourceID: id, Reason: reason, RequestID: key, AfterJSON: raw, CreatedAt: s.now()}); err != nil {
			return err
		}
		code := 200
		idem.ResponseStatus = &code
		idem.ResponseJSON = []byte(`{"ok":true}`)
		idem.ResourceType = "reward_grant"
		idem.ResourceID = id
		return r.Idempotency.Save(ctx, idem)
	})
}
