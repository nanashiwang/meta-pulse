package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
)

// maxOverviewPeriods bounds a console query. The operations console is a
// diagnostic surface; it must not be able to ask for the entire period history
// in one statement.
const maxOverviewPeriods = 200

type operationsOverviewRepository struct{ db *gorm.DB }

// ListPeriods returns periods newest first with their aggregate counters.
// The per-period aggregates come from pulse_user_period_stat, which the ingest
// path maintains in the same transaction as the Ledger, so the console and the
// accounting source cannot drift apart within a committed period.
func (r *operationsOverviewRepository) ListPeriods(ctx context.Context, limit int) ([]ports.PeriodOverview, error) {
	if limit <= 0 || limit > maxOverviewPeriods {
		limit = maxOverviewPeriods
	}
	var rows []struct {
		ID              uint64    `gorm:"column:id"`
		PeriodKey       string    `gorm:"column:period_key"`
		Status          string    `gorm:"column:status"`
		StartsAt        time.Time `gorm:"column:starts_at"`
		EndsAt          time.Time `gorm:"column:ends_at"`
		Timezone        string    `gorm:"column:timezone"`
		ConfigVersion   string    `gorm:"column:config_version"`
		RandomVersion   string    `gorm:"column:random_version"`
		RuleCount       int64     `gorm:"column:rule_count"`
		UserCount       int64     `gorm:"column:user_count"`
		UsageEventCount int64     `gorm:"column:usage_event_count"`
		EntitledTickets int64     `gorm:"column:entitled_tickets"`
		SpentTickets    int64     `gorm:"column:spent_tickets"`
	}
	// The counters are computed in correlated subqueries rather than joins so
	// a period with no rules or no users still appears, with zeros.
	if err := r.db.WithContext(ctx).Raw(`
SELECT p.id, p.period_key, p.status, p.starts_at, p.ends_at, p.timezone,
       p.config_version, p.random_version,
       (SELECT COUNT(*) FROM pulse_economics_rule r WHERE r.period_id = p.id) AS rule_count,
       (SELECT COUNT(*) FROM pulse_user_period_stat s WHERE s.period_id = p.id) AS user_count,
       (SELECT COALESCE(SUM(s.usage_event_count), 0) FROM pulse_user_period_stat s WHERE s.period_id = p.id) AS usage_event_count,
       (SELECT COALESCE(SUM(s.entitled_tickets), 0) FROM pulse_user_period_stat s WHERE s.period_id = p.id) AS entitled_tickets,
       (SELECT COALESCE(SUM(s.spent_tickets), 0) FROM pulse_user_period_stat s WHERE s.period_id = p.id) AS spent_tickets
FROM pulse_period p
ORDER BY p.starts_at DESC, p.id DESC
LIMIT ?`, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list period overview: %w", err)
	}

	result := make([]ports.PeriodOverview, 0, len(rows))
	for _, row := range rows {
		result = append(result, ports.PeriodOverview{
			ID: row.ID, Key: row.PeriodKey, Status: row.Status,
			StartsAt: row.StartsAt, EndsAt: row.EndsAt, Timezone: row.Timezone,
			ConfigVersion: row.ConfigVersion, RandomVersion: row.RandomVersion,
			RuleCount: row.RuleCount, UserCount: row.UserCount,
			UsageEventCount: row.UsageEventCount,
			EntitledTickets: row.EntitledTickets, SpentTickets: row.SpentTickets,
		})
	}
	return result, nil
}

// ListRules orders rules the way economics.Select resolves them, so the
// console shows the same precedence the ingest hot loop applies.
func (r *operationsOverviewRepository) ListRules(ctx context.Context, periodID uint64) ([]ports.EconomicsRuleOverview, error) {
	if periodID == 0 {
		return nil, fmt.Errorf("list rule overview: period id is required")
	}
	var models []economicsRuleModel
	if err := r.db.WithContext(ctx).Where("period_id = ?", periodID).
		Order("priority DESC, rule_key ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list rule overview: %w", err)
	}
	result := make([]ports.EconomicsRuleOverview, 0, len(models))
	for _, model := range models {
		result = append(result, ports.EconomicsRuleOverview{
			RuleKey: model.RuleKey, Priority: model.Priority,
			ModelPattern: model.ModelPattern, ChannelID: model.ChannelID,
			Eligible: model.Eligible, MultiplierBps: model.MultiplierBps,
			ConfigVersion: model.ConfigVersion,
		})
	}
	return result, nil
}

// ListCursors reports ingest progress per cursor. The lag is measured from the
// watermark, which is the last source instant fully ingested; a cursor with no
// watermark reports no lag rather than a misleading epoch-sized number.
func (r *operationsOverviewRepository) ListCursors(ctx context.Context, now time.Time) ([]ports.CursorOverview, error) {
	var models []cursorModel
	if err := r.db.WithContext(ctx).Order("cursor_name ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list cursor overview: %w", err)
	}
	result := make([]ports.CursorOverview, 0, len(models))
	for _, model := range models {
		overview := ports.CursorOverview{
			Name: model.CursorName, SourceSystem: model.SourceSystem,
			Value: model.CursorValue, WatermarkAt: model.WatermarkAt, Version: model.Version,
		}
		if model.WatermarkAt != nil {
			if lag := now.Sub(*model.WatermarkAt); lag > 0 {
				overview.LagSeconds = int64(lag.Seconds())
			}
		}
		result = append(result, overview)
	}
	return result, nil
}
