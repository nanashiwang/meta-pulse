package growth

import (
	"context"
	"encoding/json"
)

// RankingMember exposes public profile fields and one aggregate, never account identifiers or private data.
type RankingMember struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Avatar      any    `json:"avatar"`
	Value       int64  `json:"value,string"`
	Level       int    `json:"level"`
}

func rankingQuery(kind string) (string, error) {
	var source string
	switch kind {
	case "level":
		source = "SELECT user_id,balance AS amount FROM metar_exp_account WHERE balance>0"
	case "posts":
		source = "SELECT user_id,COUNT(*) AS amount FROM question WHERE status IN (1,2) AND `show`=1 GROUP BY user_id"
	case "answers":
		source = "SELECT a.user_id,COUNT(*) AS amount FROM answer a JOIN question q ON q.id=a.question_id WHERE a.status=1 AND q.status IN (1,2) AND q.`show`=1 GROUP BY a.user_id"
	default:
		return "", ErrInvalid
	}
	return "SELECT u.username,COALESCE(u.display_name,''),COALESCE(u.avatar,''),r.amount FROM (" + source + ") r JOIN `user` u ON u.id=r.user_id WHERE u.status=1 AND u.mail_status=1 AND NOT EXISTS (SELECT 1 FROM user_role_rel roles WHERE roles.user_id=u.id AND roles.role_id IN (2,3)) ORDER BY r.amount DESC,u.id ASC LIMIT 100", nil
}
func (s *Store) Ranking(ctx context.Context, kind string) ([]RankingMember, error) {
	query, err := rankingQuery(kind)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RankingMember, 0)
	for rows.Next() {
		var member RankingMember
		var avatar string
		if err = rows.Scan(&member.Username, &member.DisplayName, &avatar, &member.Value); err != nil {
			return nil, err
		}
		var image map[string]any
		if json.Unmarshal([]byte(avatar), &image) == nil && image != nil {
			member.Avatar = image
		} else {
			member.Avatar = avatar
		}
		if kind == "level" {
			member.Level = LevelFor(member.Value).Number
		}
		result = append(result, member)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
