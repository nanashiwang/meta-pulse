package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

var ErrPeriodWatermarkNotReady = errors.New("period watermark is not ready")

type PeriodCloseConfig struct {
	BatchSize           int
	CursorName          string
	SourceSystem        string
	RequireWatermark    bool
	EnablePeriodRewards bool
	RandomSecret        []byte
	ShadowMode          bool
	Now                 func() time.Time
}

type PeriodCloseReport struct {
	Checked        int `json:"checked"`
	Closed         int `json:"closed"`
	Deferred       int `json:"deferred"`
	Failed         int `json:"failed"`
	TicketsExpired int `json:"tickets_expired"`
	RewardsCreated int `json:"rewards_created"`
}

type PeriodCloseService struct {
	unit ports.UnitOfWork
	cfg  PeriodCloseConfig
}

func NewPeriodCloseService(unit ports.UnitOfWork, cfg PeriodCloseConfig) (*PeriodCloseService, error) {
	if unit == nil {
		return nil, errors.New("period close unit of work is nil")
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 5000 {
		return nil, errors.New("period close batch size must be between 1 and 5000")
	}
	if cfg.CursorName == "" {
		cfg.CursorName = DefaultUsageCursorName
	}
	if cfg.SourceSystem == "" {
		cfg.SourceSystem = "new-api-log"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.EnablePeriodRewards && len(cfg.RandomSecret) == 0 {
		return nil, errors.New("period reward random secret is required")
	}
	return &PeriodCloseService{unit: unit, cfg: cfg}, nil
}

func (s *PeriodCloseService) RunOnce(ctx context.Context) (PeriodCloseReport, error) {
	var report PeriodCloseReport
	var periods []period.Period
	now := s.cfg.Now()
	if err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.PeriodAdmin == nil {
			return errors.New("period admin repository is not initialized")
		}
		var err error
		periods, err = repos.PeriodAdmin.ListDueForClose(ctx, now, s.cfg.BatchSize)
		return err
	}); err != nil {
		return report, err
	}
	report.Checked = len(periods)
	for _, activity := range periods {
		closed, expired, rewards, err := s.closeOne(ctx, activity)
		if errors.Is(err, ErrPeriodWatermarkNotReady) {
			report.Deferred++
			continue
		}
		if err != nil {
			report.Failed++
			continue
		}
		if closed {
			report.Closed++
		}
		report.TicketsExpired += expired
		report.RewardsCreated += rewards
	}
	return report, nil
}

func (s *PeriodCloseService) closeOne(ctx context.Context, activity period.Period) (bool, int, int, error) {
	var expired, rewards int
	closed := false
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.PeriodAdmin == nil || repos.Cursor == nil || repos.Account == nil || repos.Ledger == nil {
			return errors.New("period close repositories are not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.cfg.CursorName, s.cfg.SourceSystem)
		if err != nil {
			return err
		}
		// ListDueForClose runs in a separate read transaction. Re-read and lock
		// the period here so another worker cannot make decisions from the stale
		// status returned by that listing transaction.
		current, err := repos.PeriodAdmin.FindByIDForUpdate(ctx, activity.ID)
		if err != nil {
			return err
		}
		if current.Status == period.StatusClosed {
			return nil
		}
		if current.Status != period.StatusActive && current.Status != period.StatusSettling {
			return fmt.Errorf("period %d has unexpected close status %s", current.ID, current.Status)
		}
		activity = current
		if s.cfg.RequireWatermark && (cursor.WatermarkAt == nil || cursor.WatermarkAt.Before(activity.EndsAt)) {
			return ErrPeriodWatermarkNotReady
		}
		if activity.Status == period.StatusActive {
			if err := repos.PeriodAdmin.Transition(ctx, activity.ID, period.StatusActive, period.StatusSettling, s.cfg.Now()); err != nil {
				return err
			}
			activity.Status = period.StatusSettling
		}
		if err := reconcilePeriod(ctx, repos, activity.ID); err != nil {
			return err
		}
		expired, err = expirePeriodTickets(ctx, repos, activity.ID, s.cfg.Now())
		if err != nil {
			return err
		}
		if s.cfg.EnablePeriodRewards {
			rewards, err = createPeriodRewards(ctx, repos, activity, s.cfg)
			if err != nil {
				return err
			}
		}
		if err := repos.PeriodAdmin.Transition(ctx, activity.ID, period.StatusSettling, period.StatusClosed, s.cfg.Now()); err != nil {
			return err
		}
		closed = true
		return nil
	})
	return closed, expired, rewards, err
}

func reconcilePeriod(ctx context.Context, repos ports.Repositories, periodID uint64) error {
	accounts, err := repos.Account.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if account.PeriodID != periodID {
			continue
		}
		entries, err := repos.Ledger.ListAccountEntries(ctx, account.UserID, account.PeriodID, account.AssetType)
		if err != nil {
			return err
		}
		rebuilt, err := ledger.Rebuild(ledger.Account{ID: account.ID, UserID: account.UserID, PeriodID: periodID, AssetType: account.AssetType}, entries)
		if err != nil {
			return fmt.Errorf("period %d ledger rebuild: %w", periodID, err)
		}
		if rebuilt.Balance != account.Balance || rebuilt.Version != account.Version {
			return fmt.Errorf("period %d account %d is out of sync", periodID, account.ID)
		}
	}
	return nil
}

func expirePeriodTickets(ctx context.Context, repos ports.Repositories, periodID uint64, now time.Time) (int, error) {
	accounts, err := repos.Account.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, account := range accounts {
		if account.PeriodID != periodID || account.AssetType != ledger.AssetTicket || account.Balance <= 0 {
			continue
		}
		grantKey := fmt.Sprintf("ticket-expire:%d:%d", periodID, account.UserID)
		if _, err := appendEntry(ctx, repos, ledger.Entry{
			UserID: account.UserID, PeriodID: periodID, AssetType: ledger.AssetTicket,
			Operation: ledger.OperationTicketExpire, Amount: -account.Balance,
			SourceType: "period_close", SourceRef: grantKey, IdempotencyKey: grantKey,
			PayloadHash: hashString(grantKey), Reason: "period closed", CreatedAt: now,
		}); err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func createPeriodRewards(ctx context.Context, repos ports.Repositories, activity period.Period, cfg PeriodCloseConfig) (int, error) {
	if repos.Reward == nil {
		return 0, errors.New("reward repository is not initialized")
	}
	accounts, err := repos.Account.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	definitions, err := repos.Reward.ListDefinitions(ctx, activity.ID)
	if err != nil {
		return 0, err
	}
	if len(definitions) == 0 {
		return 0, nil
	}
	if err := validateRewardDefinitions(definitions, activity.ConfigVersion); err != nil {
		return 0, err
	}
	budget, err := repos.Reward.GetBudgetForUpdate(ctx, activity.ID, "period_reward")
	if err != nil {
		return 0, err
	}
	created := 0
	for _, account := range accounts {
		if account.PeriodID != activity.ID || account.AssetType != ledger.AssetContribution || account.Balance <= 0 {
			continue
		}
		actionID := fmt.Sprintf("period_reward:%d:%d", activity.ID, account.UserID)
		if _, findErr := repos.Reward.FindGrantByAction(ctx, activity.ID, account.UserID, actionID); findErr == nil {
			continue
		} else if !errors.Is(findErr, ports.ErrNotFound) {
			return created, findErr
		}
		randomBytes, err := reward.Derive(cfg.RandomSecret, activity.ID, account.UserID, actionID, activity.ConfigVersion)
		if err != nil {
			return created, err
		}
		definition, err := reward.SelectWeighted(definitions, randomBytes)
		if err != nil {
			return created, err
		}
		if err := reserveBudget(&budget, definition.Amount); err != nil {
			return created, err
		}
		grantID := reward.GrantID(activity.ID, account.UserID, actionID)
		grant := ports.RewardGrant{GrantID: grantID, PeriodID: activity.ID, UserID: account.UserID, ActionID: actionID, TriggerType: "period_reward", RewardDefinitionID: definition.ID, RewardType: definition.RewardType, Amount: definition.Amount, TransferableQuota: false, BudgetType: "period_reward", RandomValue: reward.RandomHex(randomBytes), ConfigVersion: activity.ConfigVersion, Status: RewardStatusPending, SourceRef: grantID, Reason: "period reward", CreatedAt: cfg.Now()}
		persisted, err := repos.Reward.CreateGrant(ctx, grant)
		if err != nil {
			return created, err
		}
		payload, err := json.Marshal(settlementPayload{GrantID: persisted.GrantID, UserID: persisted.UserID, Amount: persisted.Amount, SourceRef: persisted.SourceRef, RewardType: persisted.RewardType})
		if err != nil {
			return created, err
		}
		outboxStatus := OutboxStatusPending
		if cfg.ShadowMode {
			outboxStatus = OutboxStatusShadow
		}
		if _, err := repos.Reward.CreateOutbox(ctx, ports.SettlementOutbox{RewardGrantID: persisted.ID, Operation: "grant", PayloadHash: canonicalJSONHash(payload), PayloadJSON: payload, Status: outboxStatus, NextAttemptAt: cfg.Now(), CreatedAt: cfg.Now()}); err != nil {
			return created, err
		}
		created++
	}
	if created > 0 {
		if err := repos.Reward.SaveBudget(ctx, budget); err != nil {
			return created, err
		}
	}
	return created, nil
}

func hashString(value string) string { return sha256HexBytes([]byte(value)) }
func sha256HexBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
