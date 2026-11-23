package awards

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const RecommendationLimit = 10
const recommendationPoolCents = 2000
const recommendationFloorCents = 300

// Ordered minor units conserve the pool for uneven shares. The earliest
// nominators receive any leftover cents; no award is rounded below the floor.
func recommendationShares(count int) []int {
	if count < 1 || count > RecommendationLimit {
		return nil
	}
	total := max(recommendationPoolCents, count*recommendationFloorCents)
	shares := make([]int, count)
	for i := range shares {
		shares[i] = total / count
		if i < total%count {
			shares[i]++
		}
	}
	return shares
}

func nominationCountLocked(ctx context.Context, tx *sql.Tx, id string) (int, error) {
	rows, err := tx.QueryContext(ctx, "SELECT user_id FROM metar_award_nomination WHERE question_id=? ORDER BY created_at,user_id LIMIT 10 FOR UPDATE", id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

type Recommendation struct {
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	Cents        int    `json:"cents,omitempty"`
	PaidAt       int64  `json:"paid_at,omitempty"`
	RewardStatus string `json:"reward_status,omitempty"`
}

func listRecommendations(ctx context.Context, tx *sql.Tx, id string, published, lock bool) ([]Recommendation, error) {
	query := "SELECT n.user_id, COALESCE(NULLIF(u.display_name,''),u.username,CAST(n.user_id AS CHAR)), COALESCE(r.cents,0), COALESCE(r.paid_at,0) FROM metar_award_nomination n LEFT JOIN user u ON u.id=n.user_id LEFT JOIN metar_award_recommendation_reward r ON r.question_id=n.question_id AND r.user_id=n.user_id WHERE n.question_id=? ORDER BY n.created_at,n.user_id LIMIT 10"
	if lock {
		query += " FOR SHARE OF n"
	}
	rows, err := tx.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recommendation
	for rows.Next() {
		var row Recommendation
		if err = rows.Scan(&row.UserID, &row.Name, &row.Cents, &row.PaidAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if published {
		for i := range out {
			row := &out[i]
			if row.Cents == 0 {
				continue
			}
			if row.PaidAt != 0 {
				row.RewardStatus = "manual_paid"
				continue
			}
			state, at, err := deliveryStatus(ctx, tx, recommendationSource(id, row.UserID))
			if err != nil {
				return nil, err
			}
			row.RewardStatus, row.PaidAt = state, at
		}
	} else {
		for i, cents := range recommendationShares(len(out)) {
			out[i].Cents = cents
		}
	}
	return out, nil
}

func freezeRecommendationRewards(ctx context.Context, tx *sql.Tx, p Period, c Candidate) error {
	if p.Type != "week" || p.Kind != "post" {
		return nil
	}
	rows, err := listRecommendations(ctx, tx, c.QuestionID, false, true)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err = tx.ExecContext(ctx, "INSERT INTO metar_award_recommendation_reward(period_key,question_id,user_id,cents) VALUES(?,?,?,?)", p.Key, c.QuestionID, row.UserID, row.Cents); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) markRecommendationPaid(ctx context.Context, tx *sql.Tx, actor string, c Command, now time.Time) error {
	var questionID, status string
	err := tx.QueryRowContext(ctx, "SELECT question_id,status FROM metar_award_winner WHERE period_type='week' AND period_key=? AND kind='post' AND subject_id=? FOR UPDATE", c.Period.Key, c.SubjectID).Scan(&questionID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "awarded" {
		return ErrForbidden
	}
	if err := ensureNotQueued(ctx, tx, recommendationSource(questionID, c.RecommenderID)); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE metar_award_recommendation_reward SET paid_by=?,paid_at=?,payment_note=? WHERE period_key=? AND question_id=? AND user_id=? AND paid_at=0", actor, now.Unix(), c.Reason, c.Period.Key, questionID, c.RecommenderID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

type RecommendationHonor struct {
	PeriodKey    string `json:"period_key"`
	QuestionID   string `json:"question_id"`
	Title        string `json:"title"`
	Cents        int    `json:"cents"`
	PaidAt       int64  `json:"paid_at"`
	RewardStatus string `json:"reward_status,omitempty"`
}

const validRecommendationSource = " FROM metar_award_recommendation_reward r JOIN metar_award_winner w ON w.period_type='week' AND w.period_key=r.period_key AND w.kind='post' AND w.question_id=r.question_id AND w.status='awarded'"

func addRecommendationHonors(ctx context.Context, tx *sql.Tx, user string, page int, out *Honors) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*)"+validRecommendationSource+" WHERE r.user_id=?", user).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		out.Badges = append(out.Badges, Badge{Name: "伯乐", Count: count, Kind: "recommendation"})
	}
	rows, err := tx.QueryContext(ctx, "SELECT r.period_key,r.question_id,COALESCE(q.title,''),r.cents,r.paid_at"+validRecommendationSource+" LEFT JOIN question q ON q.id=r.question_id AND q.status IN (1,2) AND q.`show`=1 WHERE r.user_id=? ORDER BY w.created_at DESC,r.question_id DESC LIMIT 21 OFFSET ?", user, (page-1)*20)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r RecommendationHonor
		if err = rows.Scan(&r.PeriodKey, &r.QuestionID, &r.Title, &r.Cents, &r.PaidAt); err != nil {
			return fmt.Errorf("recommendation honor: %w", err)
		}
		out.Recommendations = append(out.Recommendations, r)
	}
	if len(out.Recommendations) > 20 {
		out.More = true
		out.Recommendations = out.Recommendations[:20]
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for i := range out.Recommendations {
		row := &out.Recommendations[i]
		if row.PaidAt != 0 {
			row.RewardStatus = "manual_paid"
			continue
		}
		state, at, err := deliveryStatus(ctx, tx, recommendationSource(row.QuestionID, user))
		if err != nil {
			return err
		}
		row.RewardStatus, row.PaidAt = state, at
	}
	return nil
}
