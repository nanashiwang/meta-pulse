package awards

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/rewarddelivery"
)

var ErrRecipientUnbound = errors.New("award_recipient_unbound")

const deliveryDDL = "CREATE TABLE IF NOT EXISTS metar_award_delivery(source_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,receipt_id BIGINT UNSIGNED NOT NULL UNIQUE,user_id BIGINT UNSIGNED NOT NULL,external_id BIGINT UNSIGNED NOT NULL,actor_id BIGINT UNSIGNED NOT NULL,cents INT NOT NULL,created_at BIGINT NOT NULL,paid_at BIGINT NOT NULL DEFAULT 0,INDEX pending(paid_at,receipt_id)) ENGINE=InnoDB"

func winnerSource(p Period, user string) string {
	return "winner:" + p.Type + ":" + p.Key + ":" + p.Kind + ":" + user
}
func recommendationSource(question, user string) string {
	return "recommendation:" + question + ":" + user
}

// Existence reserves this published award forever, regardless of settlement or
// later revocation. Neither a new request key nor historical manual registration
// may create a second payment.
func deliveryStatus(ctx context.Context, tx *sql.Tx, source string) (string, int64, error) {
	var at int64
	err := tx.QueryRowContext(ctx, "SELECT paid_at FROM metar_award_delivery WHERE source_key=?", source).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return "ready", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if at == 0 {
		return "queued", 0, nil
	}
	return "settled", at, nil
}

func ensureNotQueued(ctx context.Context, tx *sql.Tx, source string) error {
	var id uint64
	err := tx.QueryRowContext(ctx, "SELECT receipt_id FROM metar_award_delivery WHERE source_key=? FOR UPDATE", source).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err == nil {
		return ErrConflict
	}
	return err
}

func (s *Store) queueDelivery(ctx context.Context, tx *sql.Tx, actor string, cmd Command, now time.Time) error {
	p := cmd.Period
	c := Candidate{SubjectID: cmd.SubjectID}
	var status string
	var amount int64
	err := tx.QueryRowContext(ctx, "SELECT user_id,question_id,answer_id,status,amount FROM metar_award_winner WHERE period_type=? AND period_key=? AND kind=? AND subject_id=? FOR UPDATE", p.Type, p.Key, p.Kind, c.SubjectID).Scan(&c.UserID, &c.QuestionID, &c.AnswerID, &status, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "awarded" {
		return ErrForbidden
	}
	if err = eligible(ctx, tx, p, &c); err != nil {
		return err
	}
	user, source := c.UserID, winnerSource(p, c.UserID)
	if cmd.Op == "rec_grant" {
		user, source = cmd.RecommenderID, recommendationSource(c.QuestionID, cmd.RecommenderID)
		var paidAt int64
		err = tx.QueryRowContext(ctx, "SELECT cents,paid_at FROM metar_award_recommendation_reward WHERE period_key=? AND question_id=? AND user_id=? FOR UPDATE", p.Key, c.QuestionID, user).Scan(&amount, &paidAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if paidAt != 0 {
			return ErrConflict
		}
		if amount < recommendationFloorCents || amount > recommendationPoolCents {
			return ErrInvalid
		}
	} else {
		var manual int64
		err = tx.QueryRowContext(ctx, "SELECT paid_at FROM metar_award_manual_payment WHERE period_type=? AND period_key=? AND kind=? AND user_id=? FOR SHARE", p.Type, p.Key, p.Kind, user).Scan(&manual)
		if err == nil {
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if amount != int64(p.Amount()) {
			return ErrInvalid
		}
		amount *= 100
	}
	if err := ensureNotQueued(ctx, tx, source); err != nil {
		return err
	}
	// Capture the protected binding. The worker rechecks it before sending; no
	// client field supplies an external identity or monetary amount.
	if err = active(ctx, tx, user, false, false); err != nil {
		return err
	}
	var external string
	err = tx.QueryRowContext(ctx, "SELECT external_id FROM user_external_login WHERE meta_pulse_user_id_guard=? AND provider='pulse_user_center' FOR SHARE", user).Scan(&external)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRecipientUnbound
	}
	if err != nil {
		return err
	}
	id, err := strconv.ParseUint(external, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != external {
		return ErrRecipientUnbound
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO metar_award_delivery(source_key,receipt_id,user_id,external_id,actor_id,cents,created_at) VALUES(?,?,?,?,?,?,?)", source, rewarddelivery.AwardID(source), user, id, actor, amount, now.Unix())
	return err
}

type Delivery struct {
	ID, User, Actor uint64
	External        string
	Cents           int64
}

func (s *Store) PendingDeliveries(ctx context.Context, after uint64) ([]Delivery, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT receipt_id,user_id,external_id,actor_id,cents FROM metar_award_delivery WHERE paid_at=0 AND receipt_id>? ORDER BY receipt_id LIMIT 20", after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var work []Delivery
	for rows.Next() {
		var d Delivery
		if err = rows.Scan(&d.ID, &d.User, &d.External, &d.Actor, &d.Cents); err != nil {
			return nil, err
		}
		work = append(work, d)
	}
	return work, rows.Err()
}

func (s *Store) ConfirmDelivery(ctx context.Context, id uint64) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE metar_award_delivery SET paid_at=? WHERE receipt_id=? AND paid_at=0", s.Now().Unix(), id)
	return err
}
