package polls

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const voterPageSize = 50

type Voter struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Choices     []int  `json:"choices"`
	UpdatedAt   int64  `json:"updated_at"`
}
type VotersPage struct {
	Items      []Voter `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// Check the current role relation, never a browser-supplied or cached role.
func pollAdministrator(ctx context.Context, tx *sql.Tx, user string) error {
	if err := active(ctx, tx, user); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT role_id FROM user_role_rel WHERE user_id=? LIMIT 2 FOR SHARE", user)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return err
		}
		return ErrForbidden
	}
	var role int
	if err = rows.Scan(&role); err != nil {
		return err
	}
	if rows.Next() {
		return ErrForbidden
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if role != 2 {
		return ErrForbidden
	}
	return nil
}

// Voters is intentionally separate from public and personal aggregate responses.
func (s *Store) Voters(ctx context.Context, id, admin, after string) (VotersPage, error) {
	out := VotersPage{Items: []Voter{}}
	if !idPattern.MatchString(id) || (after != "" && !idPattern.MatchString(after)) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = pollAdministrator(ctx, tx, admin); err != nil {
		return out, err
	}
	if _, _, err = question(ctx, tx, id, admin, false, false); err != nil {
		return out, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT 1 FROM metar_topic_poll WHERE question_id=?", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return out, err
	}
	if after == "" {
		after = "0"
	}
	rows, err := tx.QueryContext(ctx, "SELECT b.user_id,COALESCE(u.username,''),COALESCE(u.display_name,''),COALESCE(u.status,10),b.choices_json,b.updated_at FROM metar_topic_poll_ballot b LEFT JOIN user u ON u.id=b.user_id WHERE b.question_id=? AND b.user_id>? ORDER BY b.user_id ASC LIMIT ?", id, after, voterPageSize+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Voter
		var encoded string
		var status int
		if err = rows.Scan(&v.UserID, &v.Username, &v.DisplayName, &status, &encoded, &v.UpdatedAt); err != nil {
			return VotersPage{}, err
		}
		if len(out.Items) == voterPageSize {
			out.NextCursor = out.Items[len(out.Items)-1].UserID
			break
		}
		if err = json.Unmarshal([]byte(encoded), &v.Choices); err != nil {
			return VotersPage{}, err
		}
		if status == 10 {
			v.Username = ""
			v.DisplayName = ""
		}
		out.Items = append(out.Items, v)
	}
	if err = rows.Err(); err != nil {
		return VotersPage{}, err
	}
	return out, nil
}
