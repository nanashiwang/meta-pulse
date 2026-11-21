package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"time"
)

type GiftProjection struct{ DB *sql.DB }

func (g GiftProjection) GiftUsage(ctx context.Context, user uint64) (string, *int64, bool, error) {
	d := time.Now().In(time.FixedZone("Asia/Shanghai", 28800)).AddDate(0, 0, -1).Format("2006-01-02")
	if _, e := g.DB.ExecContext(ctx, "INSERT INTO pulse_gift_usage(user_id,day,verified_at) VALUES(?,?,UNIX_TIMESTAMP()) ON DUPLICATE KEY UPDATE verified_at=UNIX_TIMESTAMP()", user, d); e != nil {
		return d, nil, false, e
	}
	var q *int64
	var ready bool
	e := g.DB.QueryRowContext(ctx, "SELECT recorded,ready FROM pulse_gift_usage WHERE user_id=? AND day=?", user, d).Scan(&q, &ready)
	return d, q, ready, e
}
func (g GiftProjection) GiftPaid(ctx context.Context, user, round uint64) (bool, int64, error) {
	var paid bool
	var tickets int64
	e := g.DB.QueryRowContext(ctx, "SELECT COALESCE(g.status='settled',0),d.tickets FROM pulse_gift_delivery d LEFT JOIN pulse_reward_grant g ON g.grant_id=d.grant_id AND g.user_id=d.user_id WHERE d.user_id=? AND d.round_id=?", user, round).Scan(&paid, &tickets)
	if e == sql.ErrNoRows {
		return false, 0, nil
	}
	return paid, tickets, e
}

func (g GiftProjection) QueueGift(ctx context.Context, user uint64, v ports.GiftOrder) error {
	if user == 0 || v.Round == 0 || v.Forum == 0 || v.Actor == 0 || v.Flash < 0 || v.Flash > 1000000 || v.Tickets < 0 || v.Tickets > 100 || v.Flash+v.Tickets == 0 {
		return errors.New("invalid receipt")
	}
	if _, e := g.DB.ExecContext(ctx, "INSERT INTO pulse_gift_order(round_id,user_id,forum_user,actor,flash,tickets) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE round_id=round_id", v.Round, user, v.Forum, v.Actor, v.Flash, v.Tickets); e != nil {
		return e
	}
	var stored ports.GiftOrder
	e := g.DB.QueryRowContext(ctx, "SELECT round_id,forum_user,actor,flash,tickets FROM pulse_gift_order WHERE round_id=? AND user_id=?", v.Round, user).Scan(&stored.Round, &stored.Forum, &stored.Actor, &stored.Flash, &stored.Tickets)
	if e == nil && stored != v {
		return errors.New("receipt changed")
	}
	return e
}
