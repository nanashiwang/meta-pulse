package marketplace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const publicFrom = " FROM metar_market_item m JOIN question q ON q.id=m.question_id JOIN `user` u ON u.id=m.author_id "
const visible = "m.live_json IS NOT NULL AND m.status<>'archived' AND q.user_id=m.author_id AND q.status IN (1,2) AND q.`show`=1 AND u.status=1 AND u.mail_status=1"
const publicColumns = `m.id,m.live_json,m.version,m.embed_allowed,m.demo_status,m.featured,m.created_at,m.updated_at,m.published_at,q.title,u.username,u.display_name,u.avatar,q.answer_count,q.view_count`

func publicItem(row scanner) (Item, error) {
	var item Item
	var raw string
	err := row.Scan(&item.ID, &raw, &item.Version, &item.EmbedAllowed, &item.DemoStatus, &item.Featured, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt, &item.Title, &item.Author.Username, &item.Author.Name, &item.Author.Avatar, &item.AnswerCount, &item.ViewCount)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	err = json.Unmarshal([]byte(raw), &item.Entry)
	item.Status = "published"
	item.HasPublished = true
	return item, err
}
func escapeLike(value string) string {
	r := strings.NewReplacer("=", "==", "%", "=%", "_", "=_")
	return "%" + r.Replace(value) + "%"
}
func (s *Store) List(ctx context.Context, q Query) (Page, error) {
	if err := q.Validate(false); err != nil {
		return Page{}, err
	}
	where := visible
	args := []any{}
	if q.Kind != "" {
		where += " AND JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.kind'))=?"
		args = append(args, q.Kind)
	}
	if q.UseCase != "" {
		where += " AND JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.use_case'))=?"
		args = append(args, q.UseCase)
	}
	if q.Capability == "demo" {
		where += " AND m.demo_status='available' AND JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.demo_url'))<>''"
	}
	if q.Capability == "github" {
		where += " AND JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.github_url'))<>''"
	}
	if q.Text != "" {
		where += " AND (q.title LIKE ? ESCAPE '=' OR JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.summary')) LIKE ? ESCAPE '=' OR JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.tech_stack')) LIKE ? ESCAPE '=')"
		p := escapeLike(q.Text)
		args = append(args, p, p, p)
	}
	out := Page{Items: []Item{}, Page: q.Page, PageSize: 24}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*)`+publicFrom+`WHERE `+where, args...).Scan(&out.Count); err != nil {
		return out, err
	}
	order := "m.published_at DESC,m.id DESC"
	if q.Order == "featured" {
		order = "m.featured DESC," + order
	}
	args = append(args, out.PageSize, (q.Page-1)*out.PageSize)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+publicColumns+publicFrom+`WHERE `+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		v, e := publicItem(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}
func (s *Store) Get(ctx context.Context, id string) (Item, error) {
	if !validID(id) {
		return Item{}, ErrInvalid
	}
	return publicItem(s.DB.QueryRowContext(ctx, `SELECT `+publicColumns+publicFrom+`WHERE `+visible+` AND m.id=?`, id))
}
func (s *Store) Detail(ctx context.Context, id string) (Detail, error) {
	item, err := s.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	out := Detail{Item: item, Projects: []Item{}}
	if item.Entry.SourceIdeaID != "" {
		source, e := s.Get(ctx, item.Entry.SourceIdeaID)
		if e == nil {
			out.Source = &source
		} else if !errors.Is(e, ErrNotFound) {
			return out, e
		}
	}
	if item.Entry.Kind == "idea" {
		rows, e := s.DB.QueryContext(ctx, `SELECT `+publicColumns+publicFrom+`WHERE `+visible+` AND JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.source_idea_id'))=? ORDER BY m.published_at DESC,m.id DESC LIMIT 50`, id)
		if e != nil {
			return out, e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := publicItem(rows)
			if e != nil {
				return out, e
			}
			out.Projects = append(out.Projects, v)
		}
		if e = rows.Err(); e != nil {
			return out, e
		}
	}
	return out, nil
}
func (s *Store) PrivateList(ctx context.Context, user string, q Query, admin bool) (Page, error) {
	if err := active(ctx, s.DB, user, admin); err != nil {
		return Page{}, err
	}
	if err := q.Validate(admin); err != nil {
		return Page{}, err
	}
	where := "1=1"
	args := []any{}
	if !admin {
		where = "m.author_id=?"
		args = append(args, user)
	}
	if q.Status != "" {
		where += " AND m.status=?"
		args = append(args, q.Status)
	}
	out := Page{Items: []Item{}, Page: q.Page, PageSize: 24}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_market_item m WHERE `+where, args...).Scan(&out.Count); err != nil {
		return out, err
	}
	args = append(args, out.PageSize, (q.Page-1)*out.PageSize)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+storedColumns+` FROM metar_market_item m WHERE `+where+` ORDER BY m.updated_at DESC,m.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return out, err
	}
	list := []stored{}
	for rows.Next() {
		v, e := scanStored(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		list = append(list, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, v := range list {
		item, e := s.privateItem(ctx, s.DB, v)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
