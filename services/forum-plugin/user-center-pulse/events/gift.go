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
	Name            string     `json:"name"`
	Minutes         int64      `json:"minutes"`
	Flash           *GiftRange `json:"flash"`
	Exp             *GiftRange `json:"exp"`
	Tickets         *GiftRange `json:"tickets"`
	UsageRaw        string     `json:"usage_raw"`
	Likes           *int64     `json:"likes"`
	Comments        *int64     `json:"comments"`
	Groups          []GiftRule `json:"groups,omitempty"`
	SocialInclusive bool       `json:"social_inclusive,omitempty"`
}

type GiftRule struct {
	Flash     *GiftRange `json:"flash"`
	Exp       *GiftRange `json:"exp"`
	Tickets   *GiftRange `json:"tickets"`
	UsageRaw  string     `json:"usage_raw"`
	Likes     *int64     `json:"likes"`
	Comments  *int64     `json:"comments"`
	MaxClaims *int64     `json:"max_claims,omitempty"`
}

const maxGiftRules = 8

func (g GiftRules) legacyRule() GiftRule {
	return GiftRule{Flash: g.Flash, Exp: g.Exp, Tickets: g.Tickets, UsageRaw: g.UsageRaw, Likes: g.Likes, Comments: g.Comments}
}
func (g GiftRules) orderedRules() []GiftRule {
	if len(g.Groups) > 0 {
		return g.Groups
	}
	return []GiftRule{g.legacyRule()}
}
func (g GiftRules) valid() bool {
	if strings.TrimSpace(g.Name) == "" || len([]rune(g.Name)) > 80 || g.Minutes < 1 || g.Minutes > 43200 {
		return false
	}
	if len(g.Groups) > maxGiftRules || (g.Groups != nil && (len(g.Groups) == 0 || g.legacyRule() != (GiftRule{}))) {
		return false
	}
	for _, rule := range g.orderedRules() {
		if !rule.valid() {
			return false
		}
	}
	return true
}

func (g GiftRule) valid() bool {
	if g.MaxClaims != nil && (*g.MaxClaims < 1 || *g.MaxClaims > 1000000) {
		return false
	}
	if g.Flash == nil && g.Exp == nil && g.Tickets == nil {
		return false
	}
	for _, p := range []struct {
		r         *GiftRange
		allowZero bool
		limit     int64
	}{{g.Flash, false, 1000000}, {g.Exp, false, 1000000}, {g.Tickets, true, 100}} {
		if p.r != nil && (p.r.Min < 0 || (p.r.Min == 0 && !p.allowZero) || p.r.Max < 1 || p.r.Max < p.r.Min || p.r.Max > p.limit) {
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

// Evaluate only as far as necessary. Missing usage is a failed threshold, but
// an unavailable fact cannot demote a potentially eligible higher-priority rule.
func firstGiftMatch(rules []GiftRule, full map[int]bool, socialInclusive bool, likes, comments func() (int64, error), usage func() (*int64, error)) (int, error) {
	for index, rule := range rules {
		if full[index] {
			continue
		}
		ok := true
		for _, check := range []struct {
			limit *int64
			read  func() (int64, error)
		}{{rule.Likes, likes}, {rule.Comments, comments}} {
			if check.limit == nil {
				continue
			}
			v, err := check.read()
			if err != nil {
				return -1, err
			}
			if v < *check.limit || (!socialInclusive && v == *check.limit) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if rule.UsageRaw != "" {
			raw, err := usage()
			if err != nil {
				return -1, err
			}
			limit, _ := strconv.ParseInt(rule.UsageRaw, 10, 64)
			if raw == nil || *raw <= limit {
				continue
			}
		}
		return index, nil
	}
	return -1, nil
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
	ID           string `json:"id"`
	Name         string `json:"name"`
	End          int64  `json:"end"`
	ClaimedCount *int64 `json:"claimed_count,omitempty"`
	Unavailable  bool   `json:"unavailable,omitempty"`
	Locked       bool   `json:"locked,omitempty"`
}
type GiftReceipt struct {
	ID                              string `json:"id"`
	Flash, Exp, Tickets             int64
	FlashPaid, ExpPaid, TicketsPaid bool
	NoTicketAward                   bool `json:"no_ticket_award,omitempty"`
	RuleNumber                      int  `json:"rule_number,omitempty"`
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
	offer := &GiftOffer{ID: id, Name: g.Name, End: end}
	var oversight *GiftOffer
	if requested == "" {
		admin, err := isEventAdministrator(ctx, s.DB, user)
		if err != nil {
			return nil, nil, err
		}
		if admin {
			var count int64
			if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_gift_claim WHERE round_id=?", id).Scan(&count); err != nil {
				return nil, nil, err
			}
			offer.ClaimedCount = &count
			oversight = offer
		}
	}
	blocked := func(unavailable bool) (*GiftOffer, *GiftReceipt, error) {
		if oversight != nil {
			oversight.Locked = true
			oversight.Unavailable = unavailable
		}
		return oversight, nil, nil
	}
	ext := func(q queryer) (string, error) {
		var v string
		e := q.QueryRowContext(ctx, "SELECT b.external_id FROM user_external_login b JOIN `user` u ON u.id=b.user_id WHERE b.meta_pulse_user_id_guard=? AND b.provider='pulse_user_center' AND u.status=1 AND u.mail_status=1", user).Scan(&v)
		return v, e
	}
	external, e := ext(s.DB)
	if errors.Is(e, sql.ErrNoRows) {
		return blocked(false)
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
		e := q.QueryRowContext(ctx, "SELECT flash,exp,tickets,flash_paid,exp_paid,tickets_paid,no_ticket_award,rule_number FROM metar_gift_claim WHERE round_id=? AND user_id=? AND external_id=?", id, user, external).Scan(&v.Flash, &v.Exp, &v.Tickets, &v.FlashPaid, &v.ExpPaid, &v.TicketsPaid, &v.NoTicketAward, &v.RuleNumber)
		return &v, e
	}
	v, e := lookup(s.DB)
	if e == nil {
		if requested != "" {
			return nil, v, nil
		}
		if oversight != nil {
			return oversight, v, nil
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
	count := func(statement string) func() (int64, error) {
		return func() (int64, error) {
			var n int64
			err := s.DB.QueryRowContext(ctx, statement, user, lo.Unix(), hi.Unix()).Scan(&n)
			return n, err
		}
	}
	rules := g.orderedRules()
	full, err := s.fullGiftRules(ctx, id, rules)
	if err != nil {
		return nil, nil, err
	}
	likes := giftFact(count("SELECT COUNT(*) FROM activity a JOIN config c ON c.id=a.activity_type WHERE c.`key` IN ('question.voted_up','answer.voted_up') AND a.trigger_user_id=? AND a.cancelled=0 AND a.created_at>=FROM_UNIXTIME(?) AND a.created_at<FROM_UNIXTIME(?)"))
	comments := giftFact(count("SELECT COUNT(*) FROM comment WHERE user_id=? AND status=1 AND created_at>=FROM_UNIXTIME(?) AND created_at<FROM_UNIXTIME(?)"))
	usage := giftFact(func() (*int64, error) {
		if spend == nil {
			return nil, errors.New("usage temporarily unavailable")
		}
		return spend(ctx, external, lo.Format("2006-01-02"))
	})
	for {
		index, err := firstGiftMatch(rules, full, g.SocialInclusive, likes, comments, usage)
		if err != nil {
			if oversight != nil {
				return blocked(true)
			}
			return nil, nil, err
		}
		checked := s.Now()
		if checked.Unix() >= end || checked.Before(hi) || !checked.Before(hi.AddDate(0, 0, 1)) {
			return nil, nil, nil
		}
		if index < 0 {
			// A parallel request by this user may have filled the last slot
			// after the initial receipt lookup. Return that receipt on retry.
			if requested != "" {
				if saved, err := lookup(s.DB); err == nil {
					return nil, saved, nil
				} else if !errors.Is(err, sql.ErrNoRows) {
					return nil, nil, err
				}
			}
			return blocked(false)
		}
		if requested == "" {
			if current, e := ext(s.DB); e != nil || current != external {
				return nil, nil, ErrForbidden
			}
			return offer, nil, nil
		}
		result, err := s.claimGift(ctx, offer, user, external, index, rules[index], hi, ext, lookup)
		if errors.Is(err, errGiftRuleFull) {
			full[index] = true
			continue
		}
		if err != nil || result == nil {
			return nil, nil, err
		}
		return offer, result, nil
	}
}

// Snapshot already full rules before eligibility checks so they cannot trigger a
// usage lookup or block a lower-priority rule during a data-source outage.
func (s *Store) fullGiftRules(ctx context.Context, id string, rules []GiftRule) (map[int]bool, error) {
	full := make(map[int]bool)
	args := []any{id}
	for i, rule := range rules {
		if rule.MaxClaims != nil {
			args = append(args, i+1)
		}
	}
	if len(args) == 1 {
		return full, nil
	}
	marks := strings.Repeat("?,", len(args)-2) + "?"
	rows, err := s.DB.QueryContext(ctx, "SELECT rule_number,COUNT(*) FROM metar_gift_claim WHERE round_id=? AND rule_number IN ("+marks+") GROUP BY rule_number", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var number int
		var used int64
		if err := rows.Scan(&number, &used); err != nil {
			return nil, err
		}
		if used >= *rules[number-1].MaxClaims {
			full[number-1] = true
		}
	}
	return full, rows.Err()
}

func giftFact[T any](read func() (T, error)) func() (T, error) {
	var v T
	var err error
	done := false
	return func() (T, error) {
		if !done {
			v, err = read()
			done = true
		}
		return v, err
	}
}

var errGiftRuleFull = errors.New("gift rule full")

func (s *Store) claimGift(ctx context.Context, offer *GiftOffer, user, external string, index int, rule GiftRule, hi time.Time, ext func(queryer) (string, error), lookup func(queryer) (*GiftReceipt, error)) (*GiftReceipt, error) {
	id := offer.ID
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var end int64
	if e = tx.QueryRowContext(ctx, "SELECT ends FROM metar_gift_round WHERE id=? FOR UPDATE", id).Scan(&end); e != nil {
		return nil, e
	}
	if got, err := lookup(tx); err == nil {
		return got, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if current, e := ext(tx); e != nil || current != external {
		return nil, ErrForbidden
	}
	checked := s.Now()
	if checked.Unix() >= end || !checked.Before(hi.AddDate(0, 0, 1)) || checked.Before(hi) {
		return nil, nil
	}
	// All claims for this round share the row lock; the count and insert are atomic.
	if rule.MaxClaims != nil {
		var used int64
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_gift_claim WHERE round_id=? AND rule_number=?", id, index+1).Scan(&used); e != nil {
			return nil, e
		}
		if used >= *rule.MaxClaims {
			return nil, errGiftRuleFull
		}
	}
	v := &GiftReceipt{ID: id, RuleNumber: index + 1}
	for _, item := range []struct {
		to   *int64
		from *GiftRange
	}{{&v.Flash, rule.Flash}, {&v.Exp, rule.Exp}, {&v.Tickets, rule.Tickets}} {
		*item.to, e = giftPick(item.from)
		if e != nil {
			return nil, e
		}
	}
	v.NoTicketAward = rule.Tickets != nil && v.Tickets == 0
	_, e = tx.ExecContext(ctx, "INSERT INTO metar_gift_claim(round_id,user_id,external_id,flash,exp,tickets,no_ticket_award,rule_number,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id, user, external, v.Flash, v.Exp, v.Tickets, v.NoTicketAward, v.RuleNumber, checked.Unix())
	if e != nil {
		return nil, e
	}
	return v, tx.Commit()
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
		rs, e := s.DB.QueryContext(ctx, "SELECT c.user_id,COALESCE(NULLIF(u.display_name,''),u.username,''),c.flash,c.exp,c.tickets,c.flash_paid,c.exp_paid,c.tickets_paid,c.no_ticket_award,c.rule_number,c.created_at FROM metar_gift_claim c LEFT JOIN `user` u ON u.id=c.user_id WHERE round_id=? ORDER BY c.user_id LIMIT 50 OFFSET ?", id, (page-1)*50)
		if e != nil {
			return nil, e
		}
		defer rs.Close()
		out := []GiftPerson{}
		for rs.Next() {
			var v GiftPerson
			v.ID = id
			if e = rs.Scan(&v.User, &v.Name, &v.Flash, &v.Exp, &v.Tickets, &v.FlashPaid, &v.ExpPaid, &v.TicketsPaid, &v.NoTicketAward, &v.RuleNumber, &v.At); e != nil {
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
