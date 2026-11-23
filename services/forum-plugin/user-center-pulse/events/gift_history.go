package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
)

type GiftTierTotals struct {
	People  int64 `json:"people"`
	Flash   int64 `json:"flash"`
	Exp     int64 `json:"exp"`
	Tickets int64 `json:"tickets"`
}

type GiftTier struct {
	Number int      `json:"number"`
	Rule   GiftRule `json:"rule"`
	GiftTierTotals
}

type GiftTierOverview struct {
	Name            string         `json:"name"`
	SocialInclusive bool           `json:"social_inclusive"`
	Tiers           []GiftTier     `json:"tiers"`
	Unassigned      GiftTierTotals `json:"unassigned"`
}

func (s *Store) giftHistoryRules(ctx context.Context, id string) (GiftRules, error) {
	var rules GiftRules
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != id {
		return rules, ErrInvalid
	}
	var blob []byte
	err = s.DB.QueryRowContext(ctx, "SELECT rules FROM metar_gift_round WHERE id=?", id).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return rules, ErrInvalid
	}
	if err != nil {
		return rules, err
	}
	if json.Unmarshal(blob, &rules) != nil || !rules.valid() {
		return rules, ErrInvalid
	}
	return rules, nil
}

// GiftTierReport uses the published snapshot, never today's eligibility signals.
func (s *Store) GiftTierReport(ctx context.Context, actor, id string) (GiftTierOverview, error) {
	var out GiftTierOverview
	if err := active(ctx, s.DB, actor, true); err != nil {
		return out, err
	}
	config, err := s.giftHistoryRules(ctx, id)
	if err != nil {
		return out, err
	}
	out.Name, out.SocialInclusive = config.Name, config.SocialInclusive
	for i, rule := range config.orderedRules() {
		out.Tiers = append(out.Tiers, GiftTier{Number: i + 1, Rule: rule})
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT rule_number,COUNT(*),COALESCE(SUM(IF(flash_paid,flash,0)),0),COALESCE(SUM(IF(exp_paid,exp,0)),0),COALESCE(SUM(IF(tickets_paid,tickets,0)),0) FROM metar_gift_claim WHERE round_id=? GROUP BY rule_number", id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var number int
		var totals GiftTierTotals
		if err = rows.Scan(&number, &totals.People, &totals.Flash, &totals.Exp, &totals.Tickets); err != nil {
			return out, err
		}
		if number == 0 {
			out.Unassigned = totals
		} else if number <= len(out.Tiers) {
			out.Tiers[number-1].GiftTierTotals = totals
		} else {
			return out, ErrInvalid
		}
	}
	return out, rows.Err()
}

func (s *Store) GiftTierPeople(ctx context.Context, actor, id string, page, rule int) ([]GiftPerson, error) {
	if err := active(ctx, s.DB, actor, true); err != nil {
		return nil, err
	}
	if page < 1 || page > 1000 || rule < 0 || rule > maxGiftRules {
		return nil, ErrInvalid
	}
	config, err := s.giftHistoryRules(ctx, id)
	if err != nil {
		return nil, err
	}
	if rule > len(config.orderedRules()) {
		return nil, ErrInvalid
	}
	return s.giftPeople(ctx, id, page, &rule)
}

func (s *Store) giftPeople(ctx context.Context, id string, page int, rule *int) ([]GiftPerson, error) {
	query := "SELECT c.user_id,COALESCE(NULLIF(u.display_name,''),u.username,''),c.flash,c.exp,c.tickets,c.flash_paid,c.exp_paid,c.tickets_paid,c.no_ticket_award,c.rule_number,c.created_at FROM metar_gift_claim c LEFT JOIN `user` u ON u.id=c.user_id WHERE c.round_id=?"
	args := []any{id}
	if rule != nil {
		query += " AND c.rule_number=?"
		args = append(args, *rule)
	}
	rows, err := s.DB.QueryContext(ctx, query+" ORDER BY c.user_id LIMIT 50 OFFSET ?", append(args, (page-1)*50)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GiftPerson{}
	for rows.Next() {
		v := GiftPerson{}
		v.ID = id
		if err = rows.Scan(&v.User, &v.Name, &v.Flash, &v.Exp, &v.Tickets, &v.FlashPaid, &v.ExpPaid, &v.TicketsPaid, &v.NoTicketAward, &v.RuleNumber, &v.At); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
