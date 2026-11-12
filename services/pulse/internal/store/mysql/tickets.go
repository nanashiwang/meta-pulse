package mysql

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/big"
	"time"
)

type ticketRepository struct{ db *gorm.DB }
type ticketLotModel struct {
	ID             uint64
	UserID         uint64
	PeriodID       uint64
	MintEntryID    uint64
	Issued         int64
	Remaining      int64
	EarnedAt       int64
	QuotaExpiresAt int64
}

func (ticketLotModel) TableName() string { return "pulse_ticket_lot" }
func (r *ticketRepository) LockUser(ctx context.Context, userID uint64) error {
	if userID == 0 {
		return ports.ErrConflict
	}
	if err := r.db.WithContext(ctx).Exec("INSERT INTO pulse_ticket_user_lock (user_id) VALUES (?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)", userID).Error; err != nil {
		return err
	}
	var id uint64
	return r.db.WithContext(ctx).Raw("SELECT user_id FROM pulse_ticket_user_lock WHERE user_id=? FOR UPDATE", userID).Scan(&id).Error
}

// Reconstruct unconverted contribution from ledger facts, across rule versions.
// Locking reads see committed entries after the user mutex even under repeatable
// read. Arbitrary precision arithmetic avoids intermediate BIGINT overflow.
func (r *ticketRepository) PendingContribution(ctx context.Context, userID uint64) (int64, error) {
	var entries []struct {
		Amount    int64
		Operation string
		Threshold int64
	}
	err := r.db.WithContext(ctx).Raw(`SELECT e.amount,e.operation,p.ticket_threshold_milli AS threshold
 FROM pulse_ledger_entry e JOIN pulse_period p ON p.id=e.period_id
 WHERE e.user_id=? AND p.continuous=1 AND (e.asset_type='contribution' OR e.operation='ticket_mint') FOR SHARE`, userID).Scan(&entries).Error
	if err != nil {
		return 0, err
	}
	total := new(big.Int)
	for _, e := range entries {
		n := big.NewInt(e.Amount)
		if e.Operation == "ticket_mint" {
			n.Mul(n, big.NewInt(e.Threshold))
			total.Sub(total, n)
		} else {
			total.Add(total, n)
		}
	}
	if !total.IsInt64() {
		return 0, errors.New("contribution remainder overflow")
	}
	return total.Int64(), nil
}
func (r *ticketRepository) Mint(ctx context.Context, lot ports.TicketLot) error {
	if lot.UserID == 0 || lot.PeriodID == 0 || lot.MintEntryID == 0 || lot.Issued <= 0 || lot.Remaining != lot.Issued || !lot.QuotaExpiresAt.After(lot.EarnedAt) {
		return ports.ErrConflict
	}
	return r.db.WithContext(ctx).Create(&ticketLotModel{UserID: lot.UserID, PeriodID: lot.PeriodID, MintEntryID: lot.MintEntryID, Issued: lot.Issued, Remaining: lot.Remaining, EarnedAt: lot.EarnedAt.Unix(), QuotaExpiresAt: lot.QuotaExpiresAt.Unix()}).Error
}
func (r *ticketRepository) Spend(ctx context.Context, lot ports.TicketLot, entryID uint64) error {
	if entryID == 0 || lot.Remaining <= 0 {
		return ports.ErrConflict
	}
	if err := r.db.WithContext(ctx).Exec("INSERT INTO pulse_ticket_allocation (spend_entry_id,lot_id) VALUES (?,?)", entryID, lot.ID).Error; err != nil {
		return err
	}
	q := r.db.WithContext(ctx).Model(&ticketLotModel{}).Where("id=? AND user_id=? AND remaining=?", lot.ID, lot.UserID, lot.Remaining).Update("remaining", lot.Remaining-1)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}
func (r *ticketRepository) Period(ctx context.Context, id uint64) (period.Period, error) {
	var m periodModel
	err := r.db.WithContext(ctx).Where("id=?", id).Take(&m).Error
	return m.toDomain(), err
}

// Preview is a bounded, non-reserving read. Five distinct lots suffice for five
// tickets; multiple tickets may come from the same lot.
func (r *ticketRepository) PreviewLots(ctx context.Context, userID uint64, now time.Time) ([]ports.TicketLot, error) {
	var models []ticketLotModel
	err := r.db.WithContext(ctx).Raw(`SELECT l.* FROM pulse_ticket_lot l
 JOIN pulse_period p ON p.id=l.period_id
 JOIN pulse_account a ON a.user_id=l.user_id AND a.period_id=l.period_id AND a.asset_type='ticket'
 WHERE l.user_id=? AND l.remaining>0 AND a.balance>0 AND p.funding_policy=?
 ORDER BY (l.quota_expires_at<=?),l.earned_at,l.id LIMIT 5`, userID, period.VerifiedPaidFunding, now.Unix()).Scan(&models).Error
	result := make([]ports.TicketLot, 0, len(models))
	for _, m := range models {
		result = append(result, ticketLotDomain(m))
	}
	return result, err
}
func (r *ticketRepository) LotForUpdate(ctx context.Context, id uint64) (*ports.TicketLot, error) {
	var m ticketLotModel
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lot := ticketLotDomain(m)
	return &lot, nil
}
func ticketLotDomain(m ticketLotModel) ports.TicketLot {
	return ports.TicketLot{ID: m.ID, UserID: m.UserID, PeriodID: m.PeriodID, MintEntryID: m.MintEntryID, Issued: m.Issued, Remaining: m.Remaining, EarnedAt: time.Unix(m.EarnedAt, 0).UTC(), QuotaExpiresAt: time.Unix(m.QuotaExpiresAt, 0).UTC()}
}

func (r *ticketRepository) Statistics(ctx context.Context, ids []uint64) (map[uint64]ports.TicketStatistics, error) {
	var rows []struct {
		UserID    uint64
		Remaining int64
		Total     int64
		Spent     int64
		Removed   int64
	}
	err := r.db.WithContext(ctx).Raw(`SELECT user_id, SUM(amount) AS remaining,
 SUM(CASE WHEN amount>0 AND operation IN ('ticket_mint','ticket_bonus','ticket_adjustment') THEN amount ELSE 0 END) AS total,
 -SUM(CASE WHEN operation='ticket_spend' THEN amount ELSE 0 END) AS spent,
 -SUM(CASE WHEN amount<0 AND operation IN ('ticket_adjustment','ticket_reverse','ticket_expire') THEN amount ELSE 0 END) AS removed
 FROM pulse_ledger_entry FORCE INDEX (idx_pulse_ledger_user_asset_operation_amount) WHERE asset_type='ticket' AND user_id IN ? GROUP BY user_id`, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	var baselines []struct {
		UserID        uint64
		TotalOffset   int64
		RemovedOffset int64
	}
	if err := r.db.WithContext(ctx).Table("pulse_ticket_statistics_baseline").Where("user_id IN ?", ids).Find(&baselines).Error; err != nil {
		return nil, err
	}
	offsets := make(map[uint64][2]int64, len(baselines))
	for _, baseline := range baselines {
		offsets[baseline.UserID] = [2]int64{baseline.TotalOffset, baseline.RemovedOffset}
	}
	result := make(map[uint64]ports.TicketStatistics, len(ids))
	for _, id := range ids {
		result[id] = ports.TicketStatistics{}
	}
	for _, row := range rows {
		offset := offsets[row.UserID]
		row.Total -= offset[0]
		row.Removed -= offset[1]
		result[row.UserID] = ports.TicketStatistics{Remaining: row.Remaining, Total: row.Total, Spent: row.Spent, Removed: row.Removed}
	}
	var rewards []struct {
		UserID     uint64
		Quota      int64
		Experience int64
	}
	err = r.db.WithContext(ctx).Raw(`SELECT user_id,
 SUM(CASE WHEN reward_type='newapi_quota' THEN amount ELSE 0 END) AS quota,
 SUM(CASE WHEN reward_type='community_exp' THEN amount ELSE 0 END) AS experience
 FROM pulse_reward_grant WHERE user_id IN ? AND trigger_type='pulse' AND status='settled' AND reversed_at IS NULL GROUP BY user_id`, ids).Scan(&rewards).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rewards {
		stat := result[row.UserID]
		stat.RewardQuota = row.Quota
		stat.RewardExperience = row.Experience
		result[row.UserID] = stat
	}
	return result, nil
}
