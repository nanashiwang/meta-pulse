package events

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// All monetary fields are integer hundredths of a flash. Null ranges omit a prize.
type GiftRange struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}
type GiftRules struct {
	Name     string     `json:"name"`
	Minutes  int64      `json:"minutes"`
	Flash    *GiftRange `json:"flash"`
	Exp      *GiftRange `json:"exp"`
	Tickets  *GiftRange `json:"tickets"`
	UsageRaw string     `json:"usage_raw"`
	Likes    *int64     `json:"likes"`
	Comments *int64     `json:"comments"`
}

func (g GiftRules) valid() bool {
	if strings.TrimSpace(g.Name) == "" || len([]rune(g.Name)) > 80 || g.Minutes < 1 || g.Minutes > 43200 || g.Flash == nil && g.Exp == nil && g.Tickets == nil {
		return false
	}
	for _, p := range []struct {
		r     *GiftRange
		limit int64
	}{{g.Flash, 1000000}, {g.Exp, 1000000}, {g.Tickets, 100}} {
		if p.r != nil && (p.r.Min < 1 || p.r.Max < p.r.Min || p.r.Max > p.limit) {
			return false
		}
	}
	for _, n := range []*int64{g.Likes, g.Comments} {
		if n != nil && (*n < 0 || *n > 1000000) {
			return false
		}
	}
	if g.UsageRaw != "" {
		v, e := strconv.ParseInt(g.UsageRaw, 10, 64)
		if e != nil || v < 0 || strconv.FormatInt(v, 10) != g.UsageRaw {
			return false
		}
	}
	return true
}
func giftPick(r *GiftRange) (int64, error) {
	if r == nil {
		return 0, nil
	}
	if r.Min == r.Max {
		return r.Min, nil
	}
	n, e := rand.Int(rand.Reader, big.NewInt(1+r.Max-r.Min))
	if e != nil {
		return 0, e
	}
	return n.Int64() + r.Min, nil
}

type GiftOffer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	End  int64  `json:"end"`
}
type GiftReceipt struct {
	ID                              string `json:"id"`
	Flash, Exp, Tickets             int64
	FlashPaid, ExpPaid, TicketsPaid bool
}

var giftZone = time.FixedZone("Asia/Shanghai", 8*3600)

// A receipt identifies an immutable pick. Actual delivery is confirmed separately.
type GiftUsage func(context.Context, string, string) (*int64, error)

func (s *Store) Gift(ctx context.Context, user, requested string, spend GiftUsage) (*GiftOffer, *GiftReceipt, error) {
	if e := active(ctx, s.DB, user, false); e != nil {
		return nil, nil, e
	}
	now := s.Now()
	var id, blob string
	var end int64
	query := "SELECT id,rules,ends FROM metar_gift_round WHERE ends>? ORDER BY id DESC LIMIT 1"
	var arg any = now.Unix()
	if requested != "" {
		query = "SELECT id,rules,ends FROM metar_gift_round WHERE id=?"
		arg = requested
	}
	e := s.DB.QueryRowContext(ctx, query, arg).Scan(&id, &blob, &end)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if e != nil {
		return nil, nil, e
	}
	if requested != "" && requested != id {
		return nil, nil, ErrClosed
	}
	var g GiftRules
	if json.Unmarshal([]byte(blob), &g) != nil || !g.valid() {
		return nil, nil, ErrInvalid
	}
	ext := func(q queryer) (string, error) {
		var v string
		e := q.QueryRowContext(ctx, "SELECT b.external_id FROM user_external_login b JOIN `user` u ON u.id=b.user_id WHERE b.meta_pulse_user_id_guard=? AND b.provider='pulse_user_center' AND u.status=1 AND u.mail_status=1", user).Scan(&v)
		return v, e
	}
	external, e := ext(s.DB)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if e != nil {
		return nil, nil, e
	}
	n, e := strconv.ParseUint(external, 10, 64)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != external {
		return nil, nil, ErrForbidden
	}
	lookup := func(q queryer) (*GiftReceipt, error) {
		v := GiftReceipt{ID: id}
		e := q.QueryRowContext(ctx, "SELECT flash,exp,tickets,flash_paid,exp_paid,tickets_paid FROM metar_gift_claim WHERE round_id=? AND user_id=? AND external_id=?", id, user, external).Scan(&v.Flash, &v.Exp, &v.Tickets, &v.FlashPaid, &v.ExpPaid, &v.TicketsPaid)
		return &v, e
	}
	v, e := lookup(s.DB)
	if e == nil {
		if requested != "" {
			return nil, v, nil
		}
		return nil, nil, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, nil, e
	}
	if now.Unix() >= end {
		return nil, nil, nil
	}
	day := now.In(giftZone)
	hi := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, giftZone)
	lo := hi.AddDate(0, 0, -1)
	if g.UsageRaw != "" {
		if spend == nil {
			return nil, nil, errors.New("usage temporarily unavailable")
		}
		raw, e := spend(ctx, external, lo.Format("2006-01-02"))
		if e != nil {
			return nil, nil, e
		}
		limit, _ := strconv.ParseInt(g.UsageRaw, 10, 64)
		if raw == nil || *raw <= limit {
			return nil, nil, nil
		}
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	if requested != "" {
		if e = tx.QueryRowContext(ctx, "SELECT ends FROM metar_gift_round WHERE id=? FOR UPDATE", id).Scan(&end); e != nil {
			return nil, nil, e
		}
		if got, e := lookup(tx); e == nil {
			return nil, got, nil
		} else if !errors.Is(e, sql.ErrNoRows) {
			return nil, nil, e
		}
	}
	if current, e := ext(tx); e != nil || current != external {
		return nil, nil, ErrForbidden
	}
	for _, c := range []struct {
		gate *int64
		sql  string
	}{{g.Likes, "SELECT COUNT(*) FROM activity a JOIN config c ON c.id=a.activity_type WHERE c.`key` IN ('question.voted_up','answer.voted_up') AND a.trigger_user_id=? AND a.cancelled=0 AND a.created_at>=FROM_UNIXTIME(?) AND a.created_at<FROM_UNIXTIME(?)"}, {g.Comments, "SELECT COUNT(*) FROM comment WHERE user_id=? AND status=1 AND created_at>=FROM_UNIXTIME(?) AND created_at<FROM_UNIXTIME(?)"}} {
		if c.gate != nil {
			var count int64
			if e := tx.QueryRowContext(ctx, c.sql, user, lo.Unix(), hi.Unix()).Scan(&count); e != nil {
				return nil, nil, e
			}
			if count <= *c.gate {
				return nil, nil, nil
			}
		}
	}
	checked := s.Now()
	if checked.Unix() >= end || !checked.Before(hi.AddDate(0, 0, 1)) || checked.Before(hi) {
		return nil, nil, nil
	}
	offer := &GiftOffer{id, g.Name, end}
	if requested == "" {
		return offer, nil, nil
	}
	v = &GiftReceipt{ID: id}
	for _, item := range []struct {
		to   *int64
		from *GiftRange
	}{{&v.Flash, g.Flash}, {&v.Exp, g.Exp}, {&v.Tickets, g.Tickets}} {
		*item.to, e = giftPick(item.from)
		if e != nil {
			return nil, nil, e
		}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO metar_gift_claim(round_id,user_id,external_id,flash,exp,tickets,created_at) VALUES(?,?,?,?,?,?,?)", id, user, external, v.Flash, v.Exp, v.Tickets, checked.Unix())
	if e != nil {
		return nil, nil, e
	}
	return offer, v, tx.Commit()
}

type GiftReport struct {
	ID, Name, Actor       string
	Start, End, ServerNow int64
	People                int64
	Flash, Exp, Tickets   int64
}
type GiftPerson struct {
	User, Name string
	GiftReceipt
	At int64
}

func (s *Store) GiftReport(ctx context.Context, actor, id string, page int) (any, error) {
	if e := active(ctx, s.DB, actor, true); e != nil {
		return nil, e
	}
	if page < 1 || page > 1000 {
		return nil, ErrInvalid
	}
	if id != "" {
		if _, e := strconv.ParseUint(id, 10, 64); e != nil {
			return nil, ErrInvalid
		}
		rs, e := s.DB.QueryContext(ctx, "SELECT c.user_id,COALESCE(NULLIF(u.display_name,''),u.username,''),c.flash,c.exp,c.tickets,c.flash_paid,c.exp_paid,c.tickets_paid,c.created_at FROM metar_gift_claim c LEFT JOIN `user` u ON u.id=c.user_id WHERE round_id=? ORDER BY c.user_id LIMIT 50 OFFSET ?", id, (page-1)*50)
		if e != nil {
			return nil, e
		}
		defer rs.Close()
		out := []GiftPerson{}
		for rs.Next() {
			var v GiftPerson
			v.ID = id
			if e = rs.Scan(&v.User, &v.Name, &v.Flash, &v.Exp, &v.Tickets, &v.FlashPaid, &v.ExpPaid, &v.TicketsPaid, &v.At); e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return out, rs.Err()
	}
	rs, e := s.DB.QueryContext(ctx, "SELECT r.id,JSON_UNQUOTE(JSON_EXTRACT(r.rules,'$.name')),COALESCE(NULLIF(u.display_name,''),u.username,CAST(r.actor AS CHAR)),r.starts,r.ends,COUNT(c.user_id),COALESCE(SUM(IF(c.flash_paid,c.flash,0)),0),COALESCE(SUM(IF(c.exp_paid,c.exp,0)),0),COALESCE(SUM(IF(c.tickets_paid,c.tickets,0)),0) FROM (SELECT * FROM metar_gift_round ORDER BY id DESC LIMIT 20 OFFSET ?) r LEFT JOIN `user` u ON u.id=r.actor LEFT JOIN metar_gift_claim c ON c.round_id=r.id GROUP BY r.id,r.rules,r.actor,u.display_name,u.username,r.starts,r.ends ORDER BY r.id DESC", (page-1)*20)
	if e != nil {
		return nil, e
	}
	defer rs.Close()
	out := []GiftReport{}
	for rs.Next() {
		var v GiftReport
		v.ServerNow = s.Now().Unix()
		if e = rs.Scan(&v.ID, &v.Name, &v.Actor, &v.Start, &v.End, &v.People, &v.Flash, &v.Exp, &v.Tickets); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rs.Err()
}
