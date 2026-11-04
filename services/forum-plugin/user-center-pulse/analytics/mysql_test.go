package analytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	mysql "github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("ANALYTICS_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("ANALYTICS_INTEGRATION_DSN required")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasSuffix(cfg.DBName, "_analytics_test") {
		t.Fatal("dedicated _analytics_test schema required")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, e := sql.Open("mysql", cfg.FormatDSN())
	must(t, e)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	for _, n := range []string{"metar_ops_audit", "metar_ops_label", "metar_ops_member", "metar_ops_day", "metar_ops_visitor", "metar_ops_config", "user_role_rel", "answer", "question", "user"} {
		_, e = db.Exec("DROP TABLE IF EXISTS `" + n + "`")
		must(t, e)
	}
	for _, q := range []string{
		"CREATE TABLE `user`(id BIGINT PRIMARY KEY,username VARCHAR(50),display_name VARCHAR(30),status INT,mail_status INT,created_at TIMESTAMP NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		"CREATE TABLE user_role_rel(user_id BIGINT,role_id INT)",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,created_at TIMESTAMP NOT NULL)",
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,user_id BIGINT,question_id BIGINT,status INT,created_at TIMESTAMP NOT NULL)",
		"INSERT INTO user_role_rel VALUES(1,2)",
	} {
		_, e = db.Exec(q)
		must(t, e)
	}
	s := New(db)
	s.Now = func() time.Time { return date("2026-10-01 00:00:00") }
	must(t, s.Ensure(context.Background()))
	for id := 1; id <= 7; id++ {
		reg := "2026-10-28 10:00:00"
		if id <= 2 {
			reg = "2026-10-10 10:00:00"
		}
		status, mail := 1, 1
		if id == 4 {
			mail = 2
		}
		if id == 5 {
			status = 9
		}
		_, e = db.Exec("INSERT INTO `user` VALUES(?,?,?,?,?,FROM_UNIXTIME(?))", id, fmt.Sprintf("member%d", id), fmt.Sprintf("成员%d", id), status, mail, date(reg).Unix())
		must(t, e)
	}
	s.Now = func() time.Time { return date("2026-11-05 12:00:00") }
	return s
}
func count(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	must(t, s.DB.QueryRow(q, args...).Scan(&n))
	return n
}
func TestMySQLTrackIdentityReplayAndConversion(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	cookie := strings.Repeat("a", 64)
	s.Now = func() time.Time { return date("2026-10-28 09:00:00") }
	must(t, s.Track(ctx, cookie, "", Visit{Kind: "visit", Source: "example.com"}))
	s.Now = func() time.Time { return date("2026-10-28 10:01:00") }
	must(t, s.Track(ctx, cookie, "3", Visit{Kind: "visit"}))
	if count(t, s, "SELECT COUNT(*) FROM metar_ops_visitor WHERE converted_user=3") != 1 {
		t.Fatal("new registration not attributed")
	}
	s.Now = func() time.Time { return date("2026-10-28 10:01:29") }
	must(t, s.Track(ctx, cookie, "3", Visit{Kind: "read"}))
	if count(t, s, "SELECT COUNT(*) FROM metar_ops_day WHERE effective=1") != 0 {
		t.Fatal("early reading")
	}
	s.Now = func() time.Time { return date("2026-10-28 10:01:30") }
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Track(ctx, cookie, "3", Visit{Kind: "read"}) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	if count(t, s, "SELECT COUNT(*) FROM metar_ops_day") != 2 || count(t, s, "SELECT COUNT(*) FROM metar_ops_day WHERE effective=1") != 1 {
		t.Fatal("duplicate activity")
	}
	must(t, s.Track(ctx, strings.Repeat("b", 64), "2", Visit{Kind: "visit"}))
	if count(t, s, "SELECT COUNT(*) FROM metar_ops_visitor WHERE existing_user=1 AND converted_user IS NULL") != 1 {
		t.Fatal("existing login counted as signup")
	}
	if e := s.Track(ctx, cookie, "999", Visit{Kind: "visit"}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if e := s.Track(ctx, cookie, "5", Visit{Kind: "read"}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	s.Now = func() time.Time { return date("2026-11-05 12:00:00") }
	r, e := s.Report(ctx, "1", 30)
	must(t, e)
	if r.Conversion.Rate != (Ratio{1, 1}) || r.Conversion.Matched != 1 || *r.Conversion.Median != 3600 {
		t.Fatalf("conversion %+v", r.Conversion)
	}
	if count(t, s, "SELECT COUNT(*) FROM metar_ops_visitor WHERE id=?", cookie) != 0 {
		t.Fatal("raw cookie persisted")
	}
}
func TestMySQLLabelsConcurrencyAuditAndRevocation(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	cmd := LabelCommand{"2", 0, []string{"API 用户"}}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.SaveLabels(ctx, "1", "replay-key", cmd)
			if e == nil && v != 1 {
				e = fmt.Errorf("version %d", v)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	if count(t, s, "SELECT COUNT(*) FROM metar_ops_audit") != 1 || count(t, s, "SELECT version FROM metar_ops_member WHERE user_id=2") != 1 {
		t.Fatal("duplicate label changes")
	}
	if _, e := s.SaveLabels(ctx, "1", "another-key", cmd); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e := s.SaveLabels(ctx, "1", "replay-key", LabelCommand{"2", 0, []string{"changed"}}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	members, e := s.Members(ctx, "1", "member2", "API 用户", 1)
	must(t, e)
	if len(members.Items) != 1 || members.Items[0].Labels[0] != "API 用户" {
		t.Fatal(members)
	}
	if _, e = s.Members(ctx, "2", "", "", 1); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	_, e = s.DB.Exec("DELETE FROM user_role_rel WHERE user_id=1")
	must(t, e)
	if _, e = s.Report(ctx, "1", 30); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = s.SaveLabels(ctx, "1", "replay-key", cmd); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
}
func TestMySQLReportFactsMaturityAndExclusions(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	_, e := s.SaveLabels(ctx, "1", "exclude-test", LabelCommand{"6", 0, []string{"内部测试"}})
	must(t, e)
	for _, v := range []struct {
		id, uid, status int
		at              string
	}{{1, 2, 1, "2026-11-01 23:59:59"}, {2, 2, 2, "2026-11-02 00:00:00"}, {3, 2, 10, "2026-11-01 12:00:00"}, {4, 6, 1, "2026-11-02 12:00:00"}} {
		_, e = s.DB.Exec("INSERT INTO question VALUES(?,?,?,FROM_UNIXTIME(?))", v.id, v.uid, v.status, date(v.at).Unix())
		must(t, e)
	}
	for _, v := range []struct {
		id, uid, q int
		at         string
	}{{1, 2, 1, "2026-11-02 00:00:01"}, {2, 3, 1, "2026-11-02 01:00:00"}} {
		_, e = s.DB.Exec("INSERT INTO answer VALUES(?,?,?,1,FROM_UNIXTIME(?))", v.id, v.uid, v.q, date(v.at).Unix())
		must(t, e)
	}
	_, e = s.DB.Exec("INSERT INTO metar_ops_day VALUES('2026-11-04','u:3',3,1,1,31),('2026-11-03','u:4',4,1,1,31)")
	must(t, e)
	r, e := s.Report(ctx, "1", 30)
	must(t, e)
	if r.Summary.Registered != 5 || r.Summary.New7 != 0 || r.Summary.Authors7 != 1 || r.Summary.Posts7 != 2 || r.Summary.Answerers7 != 2 || r.Summary.Unanswered != 1 || r.Summary.Reply24h != (Ratio{1, 2}) || *r.Summary.FirstReplyMedian != 3601 {
		t.Fatalf("bad summary %+v", r.Summary)
	}
	for _, c := range r.Cohorts {
		if c.Date == "2026-10-28" {
			if c.Registered != 4 || *c.D7 != (Ratio{1, 4}) || c.WorkdaysMature {
				t.Fatalf("cohort %+v", c)
			}
		}
	}
	if *r.Summary.EffectiveDAU != 1 || *r.Summary.EffectiveWAU != 3 {
		t.Fatal("unique activity", r.Summary)
	}
	_, e = s.DB.Exec("UPDATE metar_ops_config SET started_at=?", date("2026-11-05 10:00:00").Unix())
	must(t, e)
	r, e = s.Report(ctx, "1", 30)
	must(t, e)
	if r.Summary.DAU != nil || r.Summary.WAU != nil || r.FirstFullDay != "2026-11-06" {
		t.Fatal("fabricated historical visits")
	}
}
