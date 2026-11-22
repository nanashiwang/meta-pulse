package events

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/answer/pkg/uid"
	"golang.org/x/net/html"
)

type RepliedThread struct {
	QuestionID string `json:"question_id"`
	AnswerID   string `json:"answer_id,omitempty"`
	CommentID  string `json:"comment_id,omitempty"`
	Title      string `json:"title"`
	Preview    string `json:"preview"`
	At         int64  `json:"at"`
}
type RepliedPage struct {
	Items []RepliedThread `json:"items"`
	More  bool            `json:"more"`
}

// Use the author's current visible contributions, not stale engagement records.
// Rank before paging so a thread does not occupy several slots or pages.
func (s *Store) RepliedThreads(ctx context.Context, user, query string, page int) (RepliedPage, error) {
	result := RepliedPage{Items: []RepliedThread{}}
	if page < 1 || page > 1000 || !utf8.ValidString(query) || len([]rune(query)) > 80 {
		return result, ErrInvalid
	}
	if err := active(ctx, s.DB, user, false); err != nil {
		return result, err
	}
	rows, err := s.DB.QueryContext(ctx, `WITH replies AS (
  SELECT a.question_id,a.id AS reply_id,a.id AS answer_id,a.created_at,0 AS is_comment FROM answer a WHERE a.user_id=? AND a.status=1
  UNION ALL
  SELECT c.question_id,c.id,COALESCE(a.id,0),c.created_at,1 FROM comment c
  LEFT JOIN answer a ON a.id=c.object_id AND a.question_id=c.question_id AND a.status=1
  WHERE c.user_id=? AND c.status=1 AND (c.object_id=c.question_id OR a.id IS NOT NULL)
 ), ranked AS (
  SELECT r.*,q.title,ROW_NUMBER() OVER (PARTITION BY q.id ORDER BY r.created_at DESC,r.reply_id DESC) AS rank_in_thread
  FROM replies r JOIN question q ON q.id=r.question_id WHERE q.status IN (1,2) AND q.`+"`show`"+`=1 AND LOCATE(LOWER(?),LOWER(q.title))>0
 ), page_rows AS (
  SELECT * FROM ranked WHERE rank_in_thread=1 ORDER BY created_at DESC,reply_id DESC LIMIT 21 OFFSET ?
 ) SELECT p.question_id,p.answer_id,p.reply_id,p.is_comment,p.title,UNIX_TIMESTAMP(p.created_at),COALESCE(c.parsed_text,a.parsed_text,'')
 FROM page_rows p LEFT JOIN comment c ON p.is_comment=1 AND c.id=p.reply_id LEFT JOIN answer a ON p.is_comment=0 AND a.id=p.reply_id
 ORDER BY p.created_at DESC,p.reply_id DESC`, user, user, strings.TrimSpace(query), (page-1)*20)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v RepliedThread
		var answer, reply uint64
		var comment bool
		var html string
		if err = rows.Scan(&v.QuestionID, &answer, &reply, &comment, &v.Title, &v.At, &html); err != nil {
			return result, err
		}
		v.QuestionID = uid.EnShortID(v.QuestionID)
		if answer != 0 {
			v.AnswerID = uid.EnShortID(strconv.FormatUint(answer, 10))
		}
		if comment {
			v.CommentID = uid.EnShortID(strconv.FormatUint(reply, 10))
		}
		v.Preview = replyPreview(html)
		result.Items = append(result.Items, v)
	}
	if len(result.Items) > 20 {
		result.Items = result.Items[:20]
		result.More = true
	}
	return result, rows.Err()
}

func replyPreview(rendered string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(rendered))
	var text strings.Builder
	ignored := 0
	for tokenizer.Next() != html.ErrorToken {
		tok := tokenizer.Token()
		switch tok.Type {
		case html.StartTagToken:
			if tok.Data == "script" || tok.Data == "style" {
				ignored++
			}
			if tok.Data == "br" {
				text.WriteByte(' ')
			}
		case html.EndTagToken:
			if (tok.Data == "script" || tok.Data == "style") && ignored > 0 {
				ignored--
			}
			if tok.Data == "p" || tok.Data == "li" || tok.Data == "div" {
				text.WriteByte(' ')
			}
		case html.TextToken:
			if ignored == 0 {
				text.WriteString(tok.Data)
			}
		}
	}
	clean := []rune(strings.Join(strings.Fields(text.String()), " "))
	if len(clean) > 140 {
		return string(clean[:140]) + "…"
	}
	return string(clean)
}
