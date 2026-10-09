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

// The user mutex is held by callers before these reads. Aggregate groups, not
// millions of individual tickets; the preview never reserves a ticket.
func (r *ticketRepository) Groups(ctx context.Context, userID uint64, now time.Time) ([]ports.TicketGroup, error) {
	var groups []ports.TicketGroup
	err := r.db.WithContext(ctx).Raw(`SELECT period_id, (quota_expires_at<=?) AS experience_only, SUM(remaining) AS remaining
 FROM pulse_ticket_lot WHERE user_id=? AND remaining>0
 GROUP BY period_id, experience_only ORDER BY period_id, experience_only`, now.Unix(), userID).Scan(&groups).Error
	return groups, err
}
func (r *ticketRepository) NextInGroup(ctx context.Context, userID, periodID uint64, experienceOnly bool, now time.Time) (*ports.TicketLot, error) {
	var m ticketLotModel
	q := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND period_id=? AND remaining>0", userID, periodID)
	if experienceOnly {
		q = q.Where("quota_expires_at<=?", now.Unix())
	} else {
		q = q.Where("quota_expires_at>?", now.Unix())
	}
	err := q.Order("earned_at,id").Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ports.TicketLot{ID: m.ID, UserID: m.UserID, PeriodID: m.PeriodID, MintEntryID: m.MintEntryID, Issued: m.Issued, Remaining: m.Remaining, EarnedAt: time.Unix(m.EarnedAt, 0).UTC(), QuotaExpiresAt: time.Unix(m.QuotaExpiresAt, 0).UTC()}, nil
}
