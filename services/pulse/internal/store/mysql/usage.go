package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type usageRepository struct{ db *gorm.DB }
type conflictRepository struct{ db *gorm.DB }
type cursorRepository struct{ db *gorm.DB }
type periodRepository struct{ db *gorm.DB }
type economicsRepository struct{ db *gorm.DB }
type userPeriodStatRepository struct{ db *gorm.DB }

type usageEventModel struct {
	ID                   uint64    `gorm:"column:id;primaryKey"`
	SourceSystem         string    `gorm:"column:source_system"`
	SourceEventID        string    `gorm:"column:source_event_id"`
	PayloadHash          string    `gorm:"column:payload_hash"`
	UserID               uint64    `gorm:"column:user_id"`
	PeriodID             uint64    `gorm:"column:period_id"`
	EventType            string    `gorm:"column:event_type"`
	SourceCreatedAt      time.Time `gorm:"column:source_created_at"`
	QuotaDelta           int64     `gorm:"column:quota_delta"`
	Eligible             bool      `gorm:"column:eligible"`
	EconomicsRuleID      *uint64   `gorm:"column:economics_rule_id"`
	MultiplierBps        int32     `gorm:"column:multiplier_bps"`
	ContributionMilli    int64     `gorm:"column:contribution_milli"`
	Status               string    `gorm:"column:status"`
	ModelName            string    `gorm:"column:model_name"`
	ChannelID            *uint64   `gorm:"column:channel_id"`
	RequestID            string    `gorm:"column:request_id"`
	RelatedSourceEventID string    `gorm:"column:related_source_event_id"`
	ReviewReason         string    `gorm:"column:review_reason"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (usageEventModel) TableName() string { return "pulse_usage_event" }

type ingestConflictModel struct {
	ID                  uint64    `gorm:"column:id;primaryKey"`
	SourceSystem        string    `gorm:"column:source_system"`
	SourceEventID       string    `gorm:"column:source_event_id"`
	ExistingPayloadHash string    `gorm:"column:existing_payload_hash"`
	IncomingPayloadHash string    `gorm:"column:incoming_payload_hash"`
	Reason              string    `gorm:"column:reason"`
	Status              string    `gorm:"column:status"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (ingestConflictModel) TableName() string { return "pulse_ingest_conflict" }

type cursorModel struct {
	ID           uint64     `gorm:"column:id;primaryKey"`
	CursorName   string     `gorm:"column:cursor_name"`
	SourceSystem string     `gorm:"column:source_system"`
	CursorValue  string     `gorm:"column:cursor_value"`
	WatermarkAt  *time.Time `gorm:"column:watermark_at"`
	Version      uint64     `gorm:"column:version"`
}

func (cursorModel) TableName() string { return "pulse_worker_cursor" }

type periodModel struct {
	ID            uint64    `gorm:"column:id;primaryKey"`
	PeriodKey     string    `gorm:"column:period_key"`
	Status        string    `gorm:"column:status"`
	StartsAt      time.Time `gorm:"column:starts_at"`
	EndsAt        time.Time `gorm:"column:ends_at"`
	Timezone      string    `gorm:"column:timezone"`
	ConfigVersion string    `gorm:"column:config_version"`
	RandomVersion string    `gorm:"column:random_version"`
}

func (periodModel) TableName() string { return "pulse_period" }

type economicsRuleModel struct {
	ID            uint64  `gorm:"column:id;primaryKey"`
	PeriodID      uint64  `gorm:"column:period_id"`
	RuleKey       string  `gorm:"column:rule_key"`
	Priority      int     `gorm:"column:priority"`
	ModelPattern  string  `gorm:"column:model_pattern"`
	ChannelID     *uint64 `gorm:"column:channel_id"`
	Eligible      bool    `gorm:"column:eligible"`
	MultiplierBps int32   `gorm:"column:multiplier_bps"`
	ConfigVersion string  `gorm:"column:config_version"`
}

func (economicsRuleModel) TableName() string { return "pulse_economics_rule" }

type userPeriodStatModel struct {
	ID                   uint64 `gorm:"column:id;primaryKey"`
	UserID               uint64 `gorm:"column:user_id"`
	PeriodID             uint64 `gorm:"column:period_id"`
	NetContributionMilli int64  `gorm:"column:net_contribution_milli"`
	EntitledTickets      int64  `gorm:"column:entitled_tickets"`
	SpentTickets         int64  `gorm:"column:spent_tickets"`
	UsageEventCount      uint64 `gorm:"column:usage_event_count"`
	Version              uint64 `gorm:"column:version"`
}

func (userPeriodStatModel) TableName() string { return "pulse_user_period_stat" }

func newRepositories(db *gorm.DB) ports.Repositories {
	return ports.Repositories{
		Ledger:      &ledgerRepository{db: db},
		Account:     &accountRepository{db: db},
		Usage:       &usageRepository{db: db},
		Conflict:    &conflictRepository{db: db},
		Cursor:      &cursorRepository{db: db},
		Period:      &periodRepository{db: db},
		Economics:   &economicsRepository{db: db},
		UserPeriod:  &userPeriodStatRepository{db: db},
		Reward:      &rewardRepository{db: db},
		Idempotency: &idempotencyRepository{db: db},
		Settlement:  &rewardRepository{db: db},
	}
}

func (r *ledgerRepository) FindBySource(ctx context.Context, sourceType, sourceRef string, asset ledger.AssetType) (*ledger.Entry, error) {
	var model ledgerEntryModel
	err := r.db.WithContext(ctx).Where("source_type = ? AND source_ref = ? AND asset_type = ?", sourceType, sourceRef, asset).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find ledger source: %w", err)
	}
	entry := model.toDomain()
	return &entry, nil
}

func (r *usageRepository) FindBySource(ctx context.Context, sourceSystem, sourceEventID string) (*usage.Event, error) {
	var model usageEventModel
	err := r.db.WithContext(ctx).Where("source_system = ? AND source_event_id = ?", sourceSystem, sourceEventID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find usage source: %w", err)
	}
	event := model.toDomain()
	return &event, nil
}

func (r *usageRepository) FindConsumeByRequest(ctx context.Context, userID uint64, requestID string) (*usage.Event, error) {
	if requestID == "" {
		return nil, ports.ErrNotFound
	}
	var model usageEventModel
	err := r.db.WithContext(ctx).Where("user_id = ? AND request_id = ? AND event_type = ? AND status = ?", userID, requestID, usage.EventConsume, usage.StatusAccepted).
		Order("source_created_at DESC, id DESC").Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find usage request correlation: %w", err)
	}
	event := model.toDomain()
	return &event, nil
}

func (r *usageRepository) Create(ctx context.Context, event usage.Event) (usage.Event, error) {
	model := usageModelFromDomain(event)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return usage.Event{}, ports.ErrConflict
		}
		return usage.Event{}, fmt.Errorf("create usage event: %w", err)
	}
	return model.toDomain(), nil
}

func (r *conflictRepository) Create(ctx context.Context, conflict ports.IngestConflict) error {
	model := ingestConflictModel{SourceSystem: conflict.SourceSystem, SourceEventID: conflict.SourceEventID, ExistingPayloadHash: conflict.ExistingPayloadHash, IncomingPayloadHash: conflict.IncomingPayloadHash, Reason: conflict.Reason, Status: "open"}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil
		}
		return fmt.Errorf("create ingest conflict: %w", err)
	}
	return nil
}

func (r *cursorRepository) GetOrCreateForUpdate(ctx context.Context, name, sourceSystem string) (ports.Cursor, error) {
	if err := r.db.WithContext(ctx).Exec(`
INSERT INTO pulse_worker_cursor (cursor_name, source_system, cursor_value, version)
VALUES (?, ?, '', 0)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, name, sourceSystem).Error; err != nil {
		return ports.Cursor{}, fmt.Errorf("ensure worker cursor: %w", err)
	}
	var model cursorModel
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("cursor_name = ? AND source_system = ?", name, sourceSystem).Take(&model).Error; err != nil {
		return ports.Cursor{}, fmt.Errorf("lock worker cursor: %w", err)
	}
	return ports.Cursor{Name: model.CursorName, SourceSystem: model.SourceSystem, Value: model.CursorValue, WatermarkAt: model.WatermarkAt, Version: model.Version}, nil
}

func (r *cursorRepository) Save(ctx context.Context, cursor ports.Cursor) error {
	result := r.db.WithContext(ctx).Model(&cursorModel{}).Where("cursor_name = ? AND source_system = ? AND version = ?", cursor.Name, cursor.SourceSystem, cursor.Version-1).
		Updates(map[string]any{"cursor_value": cursor.Value, "watermark_at": cursor.WatermarkAt, "version": cursor.Version})
	if result.Error != nil {
		return fmt.Errorf("save worker cursor: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *periodRepository) FindActiveAt(ctx context.Context, at time.Time) (period.Period, error) {
	var models []periodModel
	location, locationErr := time.LoadLocation("Asia/Shanghai")
	if locationErr != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	queryAt := at.In(location)
	if err := r.db.WithContext(ctx).Where("status = ? AND starts_at <= ? AND ends_at > ?", period.StatusActive, queryAt, queryAt).Order("id ASC").Find(&models).Error; err != nil {
		return period.Period{}, fmt.Errorf("find active period: %w", err)
	}
	periods := make([]period.Period, len(models))
	for i := range models {
		periods[i] = models[i].toDomain()
	}
	return period.ResolveActive(periods, at)
}

func (r *economicsRepository) ListRules(ctx context.Context, periodID uint64) ([]economics.Rule, error) {
	var models []economicsRuleModel
	if err := r.db.WithContext(ctx).Where("period_id = ?", periodID).Order("priority DESC, id ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list economics rules: %w", err)
	}
	rules := make([]economics.Rule, len(models))
	for i := range models {
		rules[i] = models[i].toDomain()
	}
	return rules, nil
}

func (r *userPeriodStatRepository) GetOrCreateForUpdate(ctx context.Context, userID, periodID uint64) (ports.UserPeriodStat, error) {
	if err := r.db.WithContext(ctx).Exec(`
INSERT INTO pulse_user_period_stat (user_id, period_id, net_contribution_milli, entitled_tickets, spent_tickets, usage_event_count, version)
VALUES (?, ?, 0, 0, 0, 0, 0)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, userID, periodID).Error; err != nil {
		return ports.UserPeriodStat{}, fmt.Errorf("ensure user period stat: %w", err)
	}
	var model userPeriodStatModel
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND period_id = ?", userID, periodID).Take(&model).Error; err != nil {
		return ports.UserPeriodStat{}, fmt.Errorf("lock user period stat: %w", err)
	}
	return model.toPort(), nil
}

func (r *userPeriodStatRepository) Save(ctx context.Context, stat ports.UserPeriodStat) error {
	if stat.ID == 0 || stat.Version == 0 {
		return fmt.Errorf("%w: invalid user period stat version", ports.ErrConflict)
	}
	result := r.db.WithContext(ctx).Model(&userPeriodStatModel{}).Where("id = ? AND version = ?", stat.ID, stat.Version-1).
		Updates(map[string]any{"net_contribution_milli": stat.NetContributionMilli, "entitled_tickets": stat.EntitledTickets, "spent_tickets": stat.SpentTickets, "usage_event_count": stat.UsageEventCount, "version": stat.Version})
	if result.Error != nil {
		return fmt.Errorf("save user period stat: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func usageModelFromDomain(event usage.Event) usageEventModel {
	return usageEventModel{ID: event.ID, SourceSystem: event.SourceSystem, SourceEventID: event.SourceEventID, PayloadHash: event.PayloadHash, UserID: event.UserID, PeriodID: event.PeriodID, EventType: string(event.EventType), SourceCreatedAt: event.SourceCreatedAt, QuotaDelta: event.QuotaDelta, Eligible: event.Eligible, EconomicsRuleID: event.EconomicsRuleID, MultiplierBps: int32(event.MultiplierBps), ContributionMilli: int64(event.ContributionMilli), Status: string(event.Status), ModelName: event.ModelName, ChannelID: uint64Ptr(event.ChannelID), RequestID: event.RequestID, RelatedSourceEventID: event.RelatedSourceEventID, ReviewReason: event.ReviewReason}
}

func (m usageEventModel) toDomain() usage.Event {
	return usage.Event{ID: m.ID, SourceSystem: m.SourceSystem, SourceEventID: m.SourceEventID, PayloadHash: m.PayloadHash, UserID: m.UserID, PeriodID: m.PeriodID, EventType: usage.EventType(m.EventType), SourceCreatedAt: m.SourceCreatedAt, QuotaDelta: m.QuotaDelta, ModelName: m.ModelName, ChannelID: derefUint64(m.ChannelID), RequestID: m.RequestID, RelatedSourceEventID: m.RelatedSourceEventID, Eligible: m.Eligible, EconomicsRuleID: m.EconomicsRuleID, MultiplierBps: money.Bps(m.MultiplierBps), ContributionMilli: money.Milli(m.ContributionMilli), Status: usage.Status(m.Status), ReviewReason: m.ReviewReason}
}

func (m periodModel) toDomain() period.Period {
	location, err := time.LoadLocation(m.Timezone)
	if err != nil || location == nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	// DATETIME is a wall-clock value. Rebuild its location explicitly instead
	// of inheriting the host timezone from the SQL driver.
	wall := func(value time.Time) time.Time {
		return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), location)
	}
	return period.Period{ID: m.ID, Key: m.PeriodKey, Status: period.Status(m.Status), StartsAt: wall(m.StartsAt), EndsAt: wall(m.EndsAt), Timezone: m.Timezone, ConfigVersion: m.ConfigVersion, RandomVersion: m.RandomVersion}
}

func (m economicsRuleModel) toDomain() economics.Rule {
	return economics.Rule{ID: m.ID, Key: m.RuleKey, Priority: m.Priority, ModelPattern: m.ModelPattern, ChannelID: m.ChannelID, Eligible: m.Eligible, MultiplierBps: money.Bps(m.MultiplierBps), ConfigVersion: m.ConfigVersion}
}

func (m userPeriodStatModel) toPort() ports.UserPeriodStat {
	return ports.UserPeriodStat{ID: m.ID, UserID: m.UserID, PeriodID: m.PeriodID, NetContributionMilli: m.NetContributionMilli, EntitledTickets: m.EntitledTickets, SpentTickets: m.SpentTickets, UsageEventCount: m.UsageEventCount, Version: m.Version}
}

func uint64Ptr(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	return &value
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}
