package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) beginRead(ctx context.Context, user string) (*sql.Tx, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	if err = active(ctx, tx, user); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
func (s *Store) Summary(ctx context.Context, user string) (Inbox, error) {
	out := Inbox{Rooms: []Room{}}
	tx, err := s.beginRead(ctx, user)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(IF(m.state='active',m.unread_count,0)),0),COALESCE(SUM(m.state='invited' AND r.closed=0),0) FROM metar_chat_member m JOIN metar_chat_room r ON r.id=m.room_id WHERE m.user_id=? AND m.state IN ('active','invited')`, user).Scan(&out.Unread, &out.Invites)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Store) Inbox(ctx context.Context, user, before string) (Inbox, error) {
	out := Inbox{Rooms: []Room{}}
	if before != "" && !validID(before) {
		return out, ErrInvalid
	}
	tx, err := s.beginRead(ctx, user)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	query := `SELECT r.id,r.kind,r.title,r.owner_id,r.seq,r.closed,r.updated_at,r.version,m.state,m.since_seq,m.read_seq,m.unread_count,COALESCE(u.id,0),COALESCE(u.username,''),COALESCE(u.display_name,''),COALESCE(u.avatar,'') FROM metar_chat_member m JOIN metar_chat_room r ON r.id=m.room_id LEFT JOIN metar_chat_member peer ON r.kind='direct' AND peer.room_id=r.id AND peer.user_id<>m.user_id LEFT JOIN user u ON u.id=peer.user_id WHERE m.user_id=? AND m.state IN ('active','invited') AND (m.state='active' OR r.closed=0)`
	args := []any{user}
	if before != "" {
		query += " AND r.id<?"
		args = append(args, before)
	}
	query += " ORDER BY r.id DESC LIMIT 51"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var r Room
		var p Person
		if err = rows.Scan(&r.ID, &r.Kind, &r.Title, &r.OwnerID, &r.Seq, &r.Closed, &r.UpdatedAt, &r.Version, &r.State, &r.Since, &r.Read, &r.Unread, &p.ID, &p.Username, &p.Name, &p.Avatar); err != nil {
			rows.Close()
			return out, err
		}
		if r.Kind == "direct" {
			r.Peer = &p
		}
		out.Rooms = append(out.Rooms, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Rooms) > 50 {
		out.Rooms = out.Rooms[:50]
		out.Next = out.Rooms[49].ID
	}
	for i := range out.Rooms {
		r := &out.Rooms[i]
		if r.State == "invited" {
			r.Seq = 0
			r.Since = 0
			r.Read = 0
			r.Unread = 0
		}
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(IF(m.state='active',m.unread_count,0)),0),COALESCE(SUM(m.state='invited' AND r.closed=0),0) FROM metar_chat_member m JOIN metar_chat_room r ON r.id=m.room_id WHERE m.user_id=? AND m.state IN ('active','invited')`, user).Scan(&out.Unread, &out.Invites)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Store) History(ctx context.Context, user, id string, before, after int64) (History, error) {
	return s.history(ctx, user, id, before, after, -1)
}
func (s *Store) Changes(ctx context.Context, user, id string, version int64) (History, error) {
	if version < 0 {
		return History{}, ErrInvalid
	}
	return s.history(ctx, user, id, 0, 0, version)
}
func (s *Store) history(ctx context.Context, user, id string, before, after, version int64) (History, error) {
	out := History{Messages: []Message{}}
	if !validID(id) || before < 0 || after < 0 || before > 0 && after > 0 {
		return out, ErrInvalid
	}
	tx, err := s.beginRead(ctx, user)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	r, err := room(ctx, tx, user, id, "FOR SHARE")
	if err != nil {
		return out, err
	}
	if r.State != "active" {
		return out, ErrForbidden
	}
	out.Room = r
	out.Cursor = r.Version
	out.Room.Members = []Member{}
	members, err := tx.QueryContext(ctx, "SELECT u.id,u.username,u.display_name,u.avatar,m.state FROM metar_chat_member m JOIN `user` u ON u.id=m.user_id WHERE m.room_id=? AND m.state IN ('active','invited') ORDER BY m.user_id", id)
	if err != nil {
		return out, err
	}
	for members.Next() {
		var m Member
		if err = members.Scan(&m.ID, &m.Username, &m.Name, &m.Avatar, &m.State); err != nil {
			members.Close()
			return out, err
		}
		out.Room.Members = append(out.Room.Members, m)
	}
	err = members.Err()
	members.Close()
	if err != nil {
		return out, err
	}
	if r.Kind == "direct" {
		for _, m := range out.Room.Members {
			if m.ID != user {
				p := m.Person
				out.Room.Peer = &p
			}
		}
	}
	query := "SELECT m.seq,u.id,u.username,u.display_name,u.avatar,m.body,m.created_at,m.removed,m.version FROM metar_chat_message m JOIN `user` u ON u.id=m.sender_id WHERE m.room_id=? AND m.seq>=?"
	args := []any{id, r.Since}
	if before > 0 {
		query += " AND m.seq<?"
		args = append(args, before)
	}
	if version >= 0 {
		query += " AND m.version>? ORDER BY m.version ASC"
		args = append(args, version)
	} else if after > 0 {
		query += " AND m.seq>?"
		args = append(args, after)
		query += " ORDER BY m.seq ASC"
	} else {
		query += " ORDER BY m.seq DESC"
	}
	query += " LIMIT 51"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var m Message
		if err = rows.Scan(&m.Seq, &m.Sender.ID, &m.Sender.Username, &m.Sender.Name, &m.Sender.Avatar, &m.Body, &m.CreatedAt, &m.Removed, &m.Version); err != nil {
			rows.Close()
			return out, err
		}
		if m.Removed {
			m.Body = ""
		}
		out.Messages = append(out.Messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Messages) > 50 {
		out.HasMore = true
		out.Messages = out.Messages[:50]
		if version >= 0 {
			out.Cursor = out.Messages[49].Version
		}
	}
	if after == 0 && version < 0 {
		for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 {
			out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
		}
	}
	return out, tx.Commit()
}
func (s *Store) People(ctx context.Context, user, query string) ([]Person, error) {
	out := []Person{}
	query = strings.TrimSpace(query)
	if !validText(query, 50) || len([]rune(query)) < 2 {
		return out, ErrInvalid
	}
	tx, err := s.beginRead(ctx, user)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	escaped := strings.NewReplacer("=", "==", "%", "=%", "_", "=_").Replace(query) + "%"
	rows, err := tx.QueryContext(ctx, "SELECT u.id,u.username,u.display_name,u.avatar FROM `user` u WHERE u.id<>? AND u.status=1 AND u.mail_status=1 AND u.username LIKE ? ESCAPE '=' AND NOT EXISTS (SELECT 1 FROM metar_chat_block b WHERE (b.user_id=? AND b.target_id=u.id) OR (b.target_id=? AND b.user_id=u.id)) ORDER BY u.username LIMIT 20", user, escaped, user, user)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Person
		if err = rows.Scan(&p.ID, &p.Username, &p.Name, &p.Avatar); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	return out, tx.Commit()
}
func (s *Store) Blocks(ctx context.Context, user string) ([]Person, error) {
	out := []Person{}
	tx, err := s.beginRead(ctx, user)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT u.id,u.username,u.display_name,u.avatar FROM metar_chat_block b JOIN `user` u ON u.id=b.target_id WHERE b.user_id=? ORDER BY b.target_id LIMIT 200", user)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Person
		if err = rows.Scan(&p.ID, &p.Username, &p.Name, &p.Avatar); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	return out, tx.Commit()
}
func (s *Store) Reports(ctx context.Context, user, before string) ([]Report, error) {
	out := []Report{}
	if before != "" && !validID(before) {
		return out, ErrInvalid
	}
	tx, err := s.beginRead(ctx, user)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = admin(ctx, tx, user); err != nil {
		return out, err
	}
	query := `SELECT id,room_id,seq,reporter_id,sender_id,body,reason,status,resolution,created_at FROM metar_chat_report WHERE status='pending'`
	args := []any{}
	if before != "" {
		query += " AND id<?"
		args = append(args, before)
	}
	query += " ORDER BY id DESC LIMIT 50"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Report
		if err = rows.Scan(&r.ID, &r.RoomID, &r.Seq, &r.Reporter, &r.Sender, &r.Body, &r.Reason, &r.Status, &r.Resolution, &r.CreatedAt); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	return out, tx.Commit()
}

// Map missing rows to a generic denial instead of exposing conversation existence.
func IsDenied(err error) bool { return errors.Is(err, ErrForbidden) || errors.Is(err, sql.ErrNoRows) }
