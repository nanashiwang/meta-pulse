package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CHAT_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("CHAT_INTEGRATION_DSN required")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasSuffix(cfg.DBName, "_chat_test") {
		t.Fatal("dedicated _chat_test schema required")
	}
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	for _, name := range []string{"metar_chat_room", "metar_chat_member", "metar_chat_message", "metar_chat_request", "metar_chat_limit", "metar_chat_block", "metar_chat_report", "user_role_rel", "user"} {
		if _, e = db.Exec("DROP TABLE IF EXISTS `" + name + "`"); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []string{
		"CREATE TABLE `user`(id BIGINT PRIMARY KEY,username VARCHAR(50),display_name VARCHAR(30),avatar VARCHAR(1024),status INT,mail_status INT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		"CREATE TABLE user_role_rel(user_id BIGINT,role_id INT)",
		"INSERT INTO `user` VALUES(1,'admin','管理员','',1,1),(2,'alice','甲','',1,1),(3,'bob','乙','',1,1),(4,'carol','丙','',1,1),(5,'inactive','未激活','',1,2),(6,'blocked','已封禁','',2,1)",
		"INSERT INTO user_role_rel VALUES(1,2)",
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s := New(db)
	s.Now = func() time.Time { return time.Unix(1800000000, 0) }
	if e = s.Ensure(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func run(t *testing.T, s *Store, u, key string, c Command) Result {
	t.Helper()
	r, e := s.Execute(context.Background(), u, key, c)
	must(t, e)
	return r
}
func TestMySQLDirectReplayAndPrivacy(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := run(t, s, "2", "direct-key", Command{Op: "direct", Target: "3"})
	reverse := run(t, s, "3", "reverse-key", Command{Op: "direct", Target: "2"})
	if reverse.RoomID != r.RoomID {
		t.Fatal("pair duplicated")
	}
	c := Command{Op: "send", RoomID: r.RoomID, Body: "你好 <script> & 🙂"}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Execute(ctx, "2", "send-key-1", c); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	history, e := s.History(ctx, "3", r.RoomID, 0, 0)
	must(t, e)
	if len(history.Messages) != 1 || history.Messages[0].Body != c.Body {
		t.Fatalf("bad history %+v", history)
	}
	unread, e := s.Summary(ctx, "3")
	must(t, e)
	if unread.Unread != 1 {
		t.Fatal(unread)
	}
	if _, e = s.History(ctx, "4", r.RoomID, 0, 0); !errors.Is(e, ErrForbidden) {
		t.Fatalf("outsider read: %v", e)
	}
	if _, e = s.Execute(ctx, "4", "intrude-1", Command{Op: "send", RoomID: r.RoomID, Body: "no"}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	c.Body = "changed"
	if _, e = s.Execute(ctx, "2", "send-key-1", c); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	run(t, s, "3", "read-key-1", Command{Op: "read", RoomID: r.RoomID, Seq: 1})
	unread, e = s.Summary(ctx, "3")
	must(t, e)
	if unread.Unread != 0 {
		t.Fatal(unread)
	}
	if _, e = s.Execute(ctx, "3", "future-read", Command{Op: "read", RoomID: r.RoomID, Seq: 2}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	_, e = s.DB.Exec("UPDATE `user` SET status=2 WHERE id=2")
	must(t, e)
	if _, e = s.Execute(ctx, "2", "send-key-1", Command{Op: "send", RoomID: r.RoomID, Body: "你好 <script> & 🙂"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("stale suspension", e)
	}
	if _, e = s.History(ctx, "2", r.RoomID, 0, 0); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
func TestMySQLGroupLifecycleAndHistoryBoundary(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := run(t, s, "2", "group-key-1", Command{Op: "group", Title: "研究组", Members: []string{"3"}})
	run(t, s, "2", "before-join", Command{Op: "send", RoomID: r.RoomID, Body: "入群前"})
	if _, e := s.History(ctx, "3", r.RoomID, 0, 0); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	run(t, s, "3", "accept-key", Command{Op: "accept", RoomID: r.RoomID})
	run(t, s, "2", "after-join", Command{Op: "send", RoomID: r.RoomID, Body: "入群后"})
	h, e := s.History(ctx, "3", r.RoomID, 0, 0)
	must(t, e)
	if len(h.Messages) != 1 || h.Messages[0].Seq != 2 {
		t.Fatal(h)
	}
	if _, e = s.Execute(ctx, "3", "invite-no", Command{Op: "invite", RoomID: r.RoomID, Target: "4"}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	run(t, s, "2", "remove-key", Command{Op: "remove", RoomID: r.RoomID, Target: "3"})
	if _, e = s.History(ctx, "3", r.RoomID, 0, 0); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = s.Execute(ctx, "3", "accept-key", Command{Op: "accept", RoomID: r.RoomID}); !errors.Is(e, ErrForbidden) {
		t.Fatal("replay leaked", e)
	}
	run(t, s, "2", "reinvite-key", Command{Op: "invite", RoomID: r.RoomID, Target: "3"})
	run(t, s, "3", "reaccept-key", Command{Op: "accept", RoomID: r.RoomID})
	h, e = s.History(ctx, "3", r.RoomID, 0, 0)
	must(t, e)
	if len(h.Messages) != 0 {
		t.Fatal("old membership leaked")
	}
	run(t, s, "2", "rename-key", Command{Op: "rename", RoomID: r.RoomID, Title: "新群名"})
	if _, e = s.Execute(ctx, "2", "owner-leave", Command{Op: "leave", RoomID: r.RoomID}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	run(t, s, "2", "transfer-key", Command{Op: "transfer", RoomID: r.RoomID, Target: "3"})
	run(t, s, "2", "leave-key", Command{Op: "leave", RoomID: r.RoomID})
	run(t, s, "3", "close-key", Command{Op: "close", RoomID: r.RoomID})
	if _, e = s.Execute(ctx, "3", "closed-send", Command{Op: "send", RoomID: r.RoomID, Body: "no"}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestMySQLBlocksInvitesAndModeration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := run(t, s, "2", "direct-key", Command{Op: "direct", Target: "3"})
	run(t, s, "2", "send-report", Command{Op: "send", RoomID: r.RoomID, Body: "举报内容"})
	run(t, s, "3", "report-key", Command{Op: "report", RoomID: r.RoomID, Seq: 1, Reason: "骚扰"})
	if _, e := s.Reports(ctx, "2", ""); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	reports, e := s.Reports(ctx, "1", "")
	must(t, e)
	if len(reports) != 1 || reports[0].Body != "举报内容" {
		t.Fatal(reports)
	}
	if _, e = s.History(ctx, "1", r.RoomID, 0, 0); !errors.Is(e, ErrForbidden) {
		t.Fatal("admin read private inbox", e)
	}
	run(t, s, "1", "resolve-key", Command{Op: "resolve_report", ReportID: reports[0].ID, Body: "remove", Reason: "确认骚扰"})
	h, e := s.History(ctx, "3", r.RoomID, 0, 0)
	must(t, e)
	if !h.Messages[0].Removed || h.Messages[0].Body != "" {
		t.Fatal(h)
	}
	run(t, s, "3", "block-key", Command{Op: "block", Target: "2"})
	if _, e = s.Execute(ctx, "2", "blocked-send", Command{Op: "send", RoomID: r.RoomID, Body: "no"}); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	if _, e = s.Execute(ctx, "2", "blocked-group", Command{Op: "group", Title: "no", Members: []string{"3"}}); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	run(t, s, "3", "unblock-key", Command{Op: "unblock", Target: "2"})
	group := run(t, s, "2", "valid-group", Command{Op: "group", Title: "ok", Members: []string{"3"}})
	run(t, s, "3", "decline-key", Command{Op: "decline", RoomID: group.RoomID})
	in, e := s.Inbox(ctx, "3", "")
	must(t, e)
	if in.Invites != 0 {
		t.Fatal(in)
	}
	for _, u := range []string{"5", "6"} {
		if _, e = s.Execute(ctx, "2", "inactive-"+u, Command{Op: "direct", Target: u}); !errors.Is(e, ErrBlocked) {
			t.Fatal(e)
		}
	}
	_, e = s.DB.Exec("UPDATE user_role_rel SET role_id=1 WHERE user_id=1")
	must(t, e)
	if _, e = s.Reports(ctx, "1", ""); !errors.Is(e, ErrForbidden) {
		t.Fatal("stale admin", e)
	}
}
func TestMySQLPaginationRecallAndLimits(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := run(t, s, "2", "direct-key", Command{Op: "direct", Target: "3"})
	for i := 1; i <= 60; i++ {
		run(t, s, "2", fmt.Sprintf("message-%03d", i), Command{Op: "send", RoomID: r.RoomID, Body: fmt.Sprint(i)})
	}
	h, e := s.History(ctx, "3", r.RoomID, 0, 0)
	must(t, e)
	if len(h.Messages) != 50 || h.Messages[0].Seq != 11 || !h.HasMore {
		t.Fatal(h)
	}
	old, e := s.History(ctx, "3", r.RoomID, 11, 0)
	must(t, e)
	if len(old.Messages) != 10 || old.HasMore {
		t.Fatal(old)
	}
	newer, e := s.History(ctx, "3", r.RoomID, 0, 1)
	must(t, e)
	if len(newer.Messages) != 50 || newer.Messages[0].Seq != 2 || !newer.HasMore {
		t.Fatal(newer)
	}
	run(t, s, "2", "recall-key", Command{Op: "recall", RoomID: r.RoomID, Seq: 60})
	if _, e = s.Execute(ctx, "3", "recall-other", Command{Op: "recall", RoomID: r.RoomID, Seq: 1}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	run(t, s, "3", "mark-partial", Command{Op: "read", RoomID: r.RoomID, Seq: 50})
	in, e := s.Summary(ctx, "3")
	must(t, e)
	if in.Unread != 10 {
		t.Fatal(in)
	}
	run(t, s, "3", "mark-stale", Command{Op: "read", RoomID: r.RoomID, Seq: 2})
	in, e = s.Summary(ctx, "3")
	must(t, e)
	if in.Unread != 10 {
		t.Fatal("cursor moved backwards")
	}
	_, e = s.DB.Exec(`UPDATE metar_chat_limit SET minute_count=120 WHERE user_id=2`)
	must(t, e)
	if _, e = s.Execute(ctx, "2", "rate-limit", Command{Op: "send", RoomID: r.RoomID, Body: "no"}); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	// A committed send is recoverable even while the rate limit is exhausted.
	run(t, s, "2", "message-001", Command{Op: "send", RoomID: r.RoomID, Body: "1"})
	people, e := s.People(ctx, "4", "al")
	must(t, e)
	if len(people) != 1 || people[0].Username != "alice" {
		t.Fatal(people)
	}
	people, e = s.People(ctx, "4", "a%")
	must(t, e)
	if len(people) != 0 {
		t.Fatal("wildcard expansion")
	}
}

func TestMySQLChangeCursorAndConcurrentPair(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	type response struct {
		r Result
		e error
	}
	answers := make(chan response, 2)
	for i, u := range []string{"2", "3"} {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			target := "2"
			if u == "2" {
				target = "3"
			}
			r, e := s.Execute(ctx, u, fmt.Sprintf("parallel-pair-%d", i), Command{Op: "direct", Target: target})
			answers <- response{r, e}
		}(i, u)
	}
	wg.Wait()
	close(answers)
	room := ""
	for a := range answers {
		must(t, a.e)
		if room != "" && room != a.r.RoomID {
			t.Fatal("concurrent pair duplicated")
		}
		room = a.r.RoomID
	}
	for i := 0; i < 55; i++ {
		run(t, s, "2", fmt.Sprintf("delta-send-%d", i), Command{Op: "send", RoomID: room, Body: fmt.Sprint(i)})
	}
	h, e := s.History(ctx, "3", room, 0, 0)
	must(t, e)
	if len(h.Messages) != 50 || h.Cursor != 55 {
		t.Fatal(h)
	}
	run(t, s, "2", "recall-old-message", Command{Op: "recall", RoomID: room, Seq: 1})
	d, e := s.Changes(ctx, "3", room, h.Cursor)
	must(t, e)
	if len(d.Messages) != 1 || d.Messages[0].Seq != 1 || !d.Messages[0].Removed || d.Messages[0].Body != "" || d.Cursor != 56 {
		t.Fatalf("redaction missing from delta: %+v", d)
	}
	first, e := s.Changes(ctx, "3", room, 0)
	must(t, e)
	if !first.HasMore || first.Cursor != 51 {
		t.Fatal(first)
	}
	last, e := s.Changes(ctx, "3", room, first.Cursor)
	must(t, e)
	if last.HasMore || len(last.Messages) != 5 || last.Cursor != 56 {
		t.Fatal(last)
	}
	run(t, s, "3", "block-inviter", Command{Op: "block", Target: "2"})
	g := run(t, s, "4", "group-invite-block", Command{Op: "group", Title: "边界", Members: []string{"2"}})
	run(t, s, "2", "accept-for-limit", Command{Op: "accept", RoomID: g.RoomID})
	_, e = s.DB.Exec("UPDATE `user` SET mail_status=2 WHERE id=2")
	must(t, e)
	if _, e = s.Changes(ctx, "2", room, 0); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("inactive delta allowed: %v", e)
	}
}

func TestMySQLCapacityAndInvitationRejection(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	g := run(t, s, "2", "invite-before-block", Command{Op: "group", Title: "邀请确认", Members: []string{"3"}})
	run(t, s, "3", "block-before-accept", Command{Op: "block", Target: "2"})
	if _, e := s.Execute(ctx, "3", "reject-blocked-accept", Command{Op: "accept", RoomID: g.RoomID}); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	run(t, s, "3", "decline-blocked-invite", Command{Op: "decline", RoomID: g.RoomID})
	run(t, s, "3", "remove-block-for-cap", Command{Op: "unblock", Target: "2"})
	for i := 10; i < 210; i++ {
		_, e := s.DB.Exec("INSERT INTO `user` VALUES(?,?,?,'',1,1)", i, fmt.Sprintf("member%d", i), "成员")
		must(t, e)
	}
	members := []string{}
	for i := 10; i < 59; i++ {
		members = append(members, fmt.Sprint(i))
	}
	full := run(t, s, "2", "full-group-capacity", Command{Op: "group", Title: "满员测试", Members: members})
	if _, e := s.Execute(ctx, "2", "overfull-group-invite", Command{Op: "invite", RoomID: full.RoomID, Target: "59"}); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	for i := 1000; i < 1100; i++ {
		_, e := s.DB.Exec(`INSERT INTO metar_chat_room(id,kind,title,owner_id,updated_at) VALUES(?,'group','fixture',3,0)`, i)
		must(t, e)
		_, e = s.DB.Exec(`INSERT INTO metar_chat_member(room_id,user_id,state) VALUES(?,3,'active')`, i)
		must(t, e)
	}
	if _, e := s.Execute(ctx, "2", "overfull-user-invite", Command{Op: "invite", RoomID: g.RoomID, Target: "3"}); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	for i := 10; i < 210; i++ {
		_, e := s.DB.Exec(`INSERT INTO metar_chat_block(user_id,target_id) VALUES(2,?)`, i)
		must(t, e)
	}
	if _, e := s.Execute(ctx, "2", "block-over-capacity", Command{Op: "block", Target: "3"}); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	run(t, s, "2", "unblock-at-capacity", Command{Op: "unblock", Target: "10"})
	run(t, s, "2", "block-freed-capacity", Command{Op: "block", Target: "3"})
	b, e := s.Blocks(ctx, "2")
	must(t, e)
	if len(b) != 200 {
		t.Fatalf("blocks hidden beyond UI limit: %d", len(b))
	}
}
