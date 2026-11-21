package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/app"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"time"
)

func deliverGifts(ctx context.Context, s *service.CommunityTicketService, db *sql.DB) error {
	type work struct {
		Round, External, Actor uint64
		Flash, Tickets         int64
	}
	rs, e := db.QueryContext(ctx, "SELECT round_id,user_id,actor,flash,tickets FROM pulse_gift_order WHERE done=0 AND retry_at<=UNIX_TIMESTAMP() ORDER BY retry_at,round_id,user_id LIMIT 20")
	if e != nil {
		return e
	}
	var rows []work
	for rs.Next() {
		var v work
		if e = rs.Scan(&v.Round, &v.External, &v.Actor, &v.Flash, &v.Tickets); e != nil {
			rs.Close()
			return e
		}
		rows = append(rows, v)
	}
	e = rs.Err()
	rs.Close()
	if e != nil {
		return e
	}
	var first error
	for _, v := range rows {
		res, e := db.ExecContext(ctx, "UPDATE pulse_gift_order SET retry_at=UNIX_TIMESTAMP()+60 WHERE round_id=? AND user_id=? AND done=0 AND retry_at<=UNIX_TIMESTAMP()", v.Round, v.External)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			continue
		}
		ok := true
		key := fmt.Sprintf("gift-%d-%d", v.Round, v.External)
		if v.Flash > 0 {
			res, e := s.GrantGiftFlash(ctx, v.External, v.Round, v.Flash)
			if e == nil {
				_, e = db.ExecContext(ctx, "INSERT INTO pulse_gift_delivery(round_id,user_id,grant_id) VALUES(?,?,?) ON DUPLICATE KEY UPDATE grant_id=VALUES(grant_id)", v.Round, v.External, res.GrantID)
			}
			if e != nil {
				first = e
				ok = false
			}
		}
		if v.Tickets > 0 {
			res, e := s.AdminGrant(ctx, fmt.Sprint(v.Actor), key, service.AdminTicketGrant{UserID: v.External, Count: v.Tickets, Reason: "限时赠礼"})
			if e == nil {
				_, e = db.ExecContext(ctx, "INSERT INTO pulse_gift_delivery(round_id,user_id,tickets) VALUES(?,?,?) ON DUPLICATE KEY UPDATE tickets=VALUES(tickets)", v.Round, v.External, res.Count)
			}
			if e != nil {
				first = e
				ok = false
			}
		}
		if ok {
			if _, e = db.ExecContext(ctx, "UPDATE pulse_gift_order SET done=1 WHERE round_id=? AND user_id=?", v.Round, v.External); e != nil {
				return e
			}
		}
	}
	return first
}
func giftUsageBatch(ctx context.Context, db *sql.DB, provider app.RuntimeConfigProvider) error {
	cfg, e := provider.Current(ctx)
	if e != nil {
		return e
	}
	if e = cfg.ValidateWorker(); e != nil {
		return e
	}
	c, e := newapi.NewBenefitClient(cfg.NewAPIInternalURL, []byte(cfg.ServiceHMACSecret), nil)
	if e != nil {
		return e
	}
	today := time.Now().In(time.FixedZone("CST", 28800)).AddDate(0, 0, -1).Format("2006-01-02")
	rs, e := db.QueryContext(ctx, "SELECT user_id FROM pulse_gift_usage WHERE day=? AND ready=0 AND verified_at>=UNIX_TIMESTAMP()-120 AND retry_at<=UNIX_TIMESTAMP() ORDER BY retry_at,user_id LIMIT 20", today)
	if e != nil {
		return e
	}
	var ids []uint64
	for rs.Next() {
		var id uint64
		if e = rs.Scan(&id); e != nil {
			rs.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rs.Err()
	rs.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		res, e := db.ExecContext(ctx, "UPDATE pulse_gift_usage SET retry_at=UNIX_TIMESTAMP()+90 WHERE user_id=? AND day=? AND ready=0 AND verified_at>=UNIX_TIMESTAMP()-120 AND retry_at<=UNIX_TIMESTAMP()", id, today)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			continue
		}
		quota, e := c.RecordedUsage(ctx, id, today)
		if e != nil || time.Now().In(time.FixedZone("CST", 28800)).AddDate(0, 0, -1).Format("2006-01-02") != today {
			continue
		}
		if _, e = db.ExecContext(ctx, "UPDATE pulse_gift_usage SET recorded=?,ready=1 WHERE user_id=? AND day=?", quota, id, today); e != nil {
			return e
		}
	}
	return ctx.Err()
}
