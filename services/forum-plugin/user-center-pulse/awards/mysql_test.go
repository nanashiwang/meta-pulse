package awards

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

func fixture(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	dsn := os.Getenv("AWARDS_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("AWARDS_INTEGRATION_DSN required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_awards_test") {
		t.Fatal("disposable _awards_test database required")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	for _, table := range []string{"metar_award_title_preference", "metar_award_manual_payment", "metar_award_audit", "metar_award_command", "metar_award_winner", "metar_award_candidate", "metar_award_nomination", "metar_award_period", "activity", "config", "answer", "question", "user_role_rel", "user"} {
		if _, err = db.Exec("DROP TABLE IF EXISTS " + "`" + table + "`"); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"CREATE TABLE user(id BIGINT PRIMARY KEY,status INT,mail_status INT,username VARCHAR(100),display_name VARCHAR(100)) ENGINE=InnoDB",
		"INSERT INTO user VALUES(1,1,1,'owner','作者'),(2,1,1,'reader','读者'),(3,1,1,'admin','管理员'),(4,2,1,'banned','封禁'),(5,1,2,'inactive','未激活'),(6,1,1,'responder','回答者')",
		"CREATE TABLE user_role_rel(user_id BIGINT,role_id INT) ENGINE=InnoDB", "INSERT INTO user_role_rel VALUES(1,1),(2,1),(3,2),(4,2),(5,2),(6,1)",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,title VARCHAR(200),original_text TEXT,status INT,`show` INT,created_at DATETIME,accepted_answer_id BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB",
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,user_id BIGINT,question_id BIGINT,status INT,accepted INT,original_text TEXT) ENGINE=InnoDB",
		"CREATE TABLE config(id INT PRIMARY KEY,`key` VARCHAR(100)) ENGINE=InnoDB", "INSERT INTO config VALUES(1,'answer.accepted')",
		"CREATE TABLE activity(id BIGINT AUTO_INCREMENT PRIMARY KEY,object_id BIGINT,user_id BIGINT,trigger_user_id BIGINT,activity_type INT,cancelled INT DEFAULT 0,created_at DATETIME) ENGINE=InnoDB",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := date(t, "2026-11-09 12:00:00")
	s := New(db)
	s.Now = func() time.Time { return now }
	if err = s.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 125; i++ {
		if _, err = db.Exec("INSERT INTO question(id,user_id,title,status,`show`,created_at) VALUES(?,1,'讨论',1,1,?)", i, now.Add(-time.Hour).UTC()); err != nil {
			t.Fatal(err)
		}
	}
	return s, &now
}
func execute(t *testing.T, s *Store, actor, key string, c Command, admin bool) Result {
	t.Helper()
	r, err := s.Execute(context.Background(), actor, key, c, admin)
	if err != nil {
		t.Fatal(c.Op, err)
	}
	return r
}
func reviewCommand(op, kind, subject string, version int64) Command {
	return Command{Op: op, Period: Period{"week", "2026-11-09", kind}, SubjectID: subject, Reason: "有具体证据与实际帮助", CriterionOne: true, CriterionTwo: true, Version: version}
}
func TestNominationQuotaReplayAndConcurrencyMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	c := Command{Op: "nominate", QuestionID: "100"}
	for _, user := range []string{"1", "4", "5"} {
		if _, err := s.Execute(ctx, user, "nomination-denied1", c, false); !errors.Is(err, ErrForbidden) {
			t.Fatal(user, err)
		}
	}
	execute(t, s, "2", "nomination-first1", c, false)
	if !execute(t, s, "2", "nomination-repeat1", c, false).Already {
		t.Fatal("duplicate consumes quota")
	}
	if _, err := s.Execute(ctx, "2", "nomination-first1", Command{Op: "nominate", QuestionID: "101"}, false); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Execute(ctx, "2", fmt.Sprintf("quota-race-%016d", i), Command{Op: "nominate", QuestionID: fmt.Sprint(101 + i)}, false)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else if !errors.Is(err, ErrQuota) {
			t.Fatal(err)
		}
	}
	if ok != 1 {
		t.Fatal(ok)
	}
	for d := 1; d < 5; d++ {
		*now = now.AddDate(0, 0, 1)
		for n := 0; n < 2; n++ {
			execute(t, s, "2", fmt.Sprintf("weekly-fill-%016d", d*2+n), Command{Op: "nominate", QuestionID: fmt.Sprint(110 + d*2 + n)}, false)
		}
	}
	*now = now.AddDate(0, 0, 1)
	if _, err := s.Execute(ctx, "2", "weekly-exhausted1", Command{Op: "nominate", QuestionID: "124"}, false); !errors.Is(err, ErrQuota) {
		t.Fatal(err)
	}
	status, err := s.Status(ctx, "2", "124")
	if err != nil || status.WeeklyRemaining != 0 || status.DailyRemaining != 2 {
		t.Fatal(status, err)
	}
}
func TestWeeklyPublicationNeverPaysAndFreezesMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	execute(t, s, "2", "post-nomination01", Command{Op: "nominate", QuestionID: "100"}, false)
	selectCmd := reviewCommand("select", "post", "100", 0)
	if _, err := s.Execute(ctx, "3", "review-too-early1", selectCmd, true); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	*now = date(t, "2026-11-18 00:00:00")
	execute(t, s, "3", "review-selected1", selectCmd, true)
	execute(t, s, "3", "publish-weekly01", reviewCommand("publish", "post", "", 1), true)
	list, err := s.List(ctx, "", selectCmd.Period, 1, false)
	if err != nil || len(list.Items) != 1 || list.Items[0].Amount != 50 || list.Items[0].RewardStatus != "manual_pending" {
		t.Fatal(list, err)
	}
	execute(t, s, "3", "publish-weekly01", reviewCommand("publish", "post", "", 1), true)
	if _, err = s.Execute(ctx, "3", "late-review-0001", reviewCommand("reject", "post", "100", 2), true); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	execute(t, s, "3", "revoke-weekly001", reviewCommand("revoke", "post", "100", 2), true)
	list, err = s.List(ctx, "", selectCmd.Period, 1, false)
	if err != nil || len(list.Items) != 0 {
		t.Fatal(list, err)
	}
}
func TestAcceptedResponseUsesFirstAuthorAcceptanceMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	s.DB.Exec("INSERT INTO answer(id,user_id,question_id,status,accepted) VALUES(200,6,100,1,2),(201,1,101,1,2)")
	s.DB.Exec("UPDATE question SET accepted_answer_id=200 WHERE id=100")
	if _, err := s.DB.Exec("INSERT INTO activity(object_id,user_id,trigger_user_id,activity_type,created_at) VALUES(200,6,1,1,?),(201,1,1,1,?)", now.UTC(), now.UTC()); err != nil {
		t.Fatal(err)
	}
	p := Period{"week", "2026-11-09", "response"}
	execute(t, s, "3", "sync-responses01", Command{Op: "sync", Period: p}, true)
	list, err := s.List(ctx, "3", p, 1, true)
	if err != nil || len(list.Items) != 1 || !list.Items[0].Eligible {
		t.Fatal(list, err)
	}
	s.DB.Exec("UPDATE answer SET accepted=1 WHERE id=200")
	list, err = s.List(ctx, "3", p, 1, true)
	if err != nil || list.Items[0].Eligible {
		t.Fatal("cancelled acceptance eligible", err)
	}
	s.DB.Exec("UPDATE answer SET accepted=2 WHERE id=200")
	*now = date(t, "2026-11-18 12:00:00")
	execute(t, s, "3", "sync-next-week01", Command{Op: "sync", Period: Period{"week", "2026-11-16", "response"}}, true)
	list, err = s.List(ctx, "3", Period{"week", "2026-11-16", "response"}, 1, true)
	if err != nil || len(list.Items) != 0 {
		t.Fatal("reaccept moved week", err)
	}
	execute(t, s, "3", "select-response1", reviewCommand("select", "response", "200", 1), true)
	execute(t, s, "3", "publish-response1", reviewCommand("publish", "response", "", 2), true)
	s.DB.Exec("UPDATE question SET `show`=2 WHERE id=100")
	list, err = s.List(ctx, "", p, 1, false)
	if err != nil || len(list.Items) != 0 {
		t.Fatal("hidden content leaked", err)
	}
}

func TestAwardCapsAndMonthlySourceReviewMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	*now = date(t, "2026-11-18 12:00:00")
	p := Period{"week", "2026-11-09", "post"}
	for i := 0; i < 11; i++ {
		user := 20 + i
		q := 200 + i
		s.DB.Exec("INSERT INTO user VALUES(?,1,1,'member','成员')", user)
		s.DB.Exec("INSERT INTO question(id,user_id,title,status,`show`,created_at) VALUES(?,?,'内容',1,1,?)", q, user, now.UTC())
		if _, err := s.DB.Exec("INSERT INTO metar_award_candidate(period_type,period_key,kind,subject_id,user_id,question_id,reason,created_at) VALUES('week',?,'post',?,?,?,'',1)", p.Key, q, user, q); err != nil {
			t.Fatal(err)
		}
		c := reviewCommand("select", "post", fmt.Sprint(q), int64(i))
		_, err := s.Execute(ctx, "3", fmt.Sprintf("select-cap-%016d", i), c, true)
		if i < 10 && err != nil {
			t.Fatal(err)
		}
		if i == 10 && !errors.Is(err, ErrQuota) {
			t.Fatal("cap", err)
		}
	}
	execute(t, s, "3", "publish-cap-00001", reviewCommand("publish", "post", "", 10), true)
	list, err := s.List(ctx, "", p, 1, false)
	if err != nil || len(list.Items) != 10 {
		t.Fatal(list, err)
	}
}
func TestMonthWaitsDeduplicatesAndRechecksSourcesMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	*now = date(t, "2026-12-09 12:00:00")
	p := Period{"month", "2026-11", "post"}
	syncCmd := Command{Op: "sync", Period: p}
	if _, err := s.Execute(ctx, "3", "month-too-soon001", syncCmd, true); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	weeks, _ := MonthWeeks(p)
	for _, week := range weeks {
		if _, err := s.DB.Exec("INSERT INTO metar_award_period(period_type,period_key,kind,status) VALUES('week',?,'post','published')", week); err != nil {
			t.Fatal(err)
		}
	}
	for i, week := range weeks[:2] {
		if _, err := s.DB.Exec("INSERT INTO metar_award_winner(period_type,period_key,kind,user_id,subject_id,question_id,month_key,reason,amount,created_at) VALUES('week',?,'post',1,?,?,'2026-11','有价值',50,1)", week, 100+i, 100+i); err != nil {
			t.Fatal(err)
		}
	}
	execute(t, s, "3", "month-sync-000001", syncCmd, true)
	list, err := s.List(ctx, "3", p, 1, true)
	if err != nil || len(list.Items) != 1 || len(list.Items[0].Sources) != 2 {
		t.Fatal(list, err)
	}
	c := Command{Op: "select", Period: p, SubjectID: "1", Version: 1, Reason: "有可靠内容和有效讨论", CriterionOne: true, CriterionTwo: true}
	execute(t, s, "3", "month-select0001", c, true)
	execute(t, s, "3", "month-publish001", Command{Op: "publish", Period: p, Version: 2}, true)
	list, err = s.List(ctx, "", p, 1, false)
	if err != nil || len(list.Items) != 1 || list.Items[0].Amount != 100 || list.Items[0].RewardStatus != "manual_pending" {
		t.Fatal(list, err)
	}
	execute(t, s, "3", "week-revoke-source", Command{Op: "revoke", Period: Period{"week", weeks[0], "post"}, SubjectID: "100", Reason: "来源撤销"}, true)
	list, err = s.List(ctx, "3", p, 1, true)
	if err != nil || list.Items[0].Status != "review_required" {
		t.Fatal(list, err)
	}
	c.Op = "reconfirm"
	c.Version = list.Version
	execute(t, s, "3", "month-reconfirm1", c, true)
	list, err = s.List(ctx, "", p, 1, false)
	if err != nil || len(list.Items) != 1 || len(list.Items[0].Sources) != 1 {
		t.Fatal(list, err)
	}
	if _, err = s.DB.Exec("UPDATE user_role_rel SET role_id=1 WHERE user_id=3"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.List(ctx, "3", p, 1, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("demoted admin", err)
	}
}

func TestEditedContentRequiresReviewAgainMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	execute(t, s, "2", "edit-post-nominate", Command{Op: "nominate", QuestionID: "100"}, false)
	*now = date(t, "2026-11-18 12:00:00")
	execute(t, s, "3", "edit-first-review", reviewCommand("select", "post", "100", 0), true)
	if _, err := s.DB.Exec("UPDATE question SET original_text='修改后的内容' WHERE id=100"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, "3", "edit-publish-deny", reviewCommand("publish", "post", "", 1), true); !errors.Is(err, ErrConflict) {
		t.Fatal("edited content retained stale review", err)
	}
	execute(t, s, "3", "edit-re-review001", reviewCommand("select", "post", "100", 1), true)
	execute(t, s, "3", "edit-publish-new1", reviewCommand("publish", "post", "", 2), true)
}

func TestHonorsManualPaymentAndRevocationMySQL(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	execute(t, s, "2", "honor-nominate01", Command{Op: "nominate", QuestionID: "100"}, false)
	*now = date(t, "2026-11-18 12:00:00")
	execute(t, s, "3", "honor-select0001", reviewCommand("select", "post", "100", 0), true)
	execute(t, s, "3", "honor-publish001", reviewCommand("publish", "post", "", 1), true)
	h, err := s.Honors(ctx, "1", "", 1)
	if err != nil || len(h.Titles) != 1 || len(h.Badges) != 1 || h.Badges[0].Count != 1 || h.Selected != nil {
		t.Fatal(h, err)
	}
	if h.Titles[0].ExpiresAt != now.AddDate(0, 0, 7).Unix() {
		t.Fatal(h)
	}
	execute(t, s, "1", "honor-choose0001", Command{Op: "title", TitleKey: "post-week"}, false)
	public, err := s.Honors(ctx, "", "owner", 1)
	if err != nil || public.Selected == nil || public.Selected.Name != "优秀作者" || public.Titles != nil || public.Badges != nil {
		t.Fatal(public, err)
	}
	pay := reviewCommand("paid", "post", "100", 2)
	pay.Reason = "人工转账记录001"
	execute(t, s, "3", "manual-paid00001", pay, true)
	execute(t, s, "3", "manual-paid00001", pay, true)
	list, err := s.List(ctx, "3", pay.Period, 1, true)
	if err != nil || list.Items[0].RewardStatus != "manual_paid" || list.Items[0].PaymentNote != pay.Reason {
		t.Fatal(list, err)
	}
	// Monthly grant is a second medal and a separate selectable title version.
	if _, err = s.DB.Exec("INSERT INTO metar_award_winner(period_type,period_key,kind,user_id,subject_id,question_id,answer_id,month_key,status,reason,amount,created_at) VALUES('month','2026-11','post',1,1,100,0,'2026-11','awarded','月奖',100,?)", now.Unix()); err != nil {
		t.Fatal(err)
	}
	h, err = s.Honors(ctx, "1", "", 1)
	if err != nil || len(h.Titles) != 2 || h.Badges[0].Count != 2 {
		t.Fatal(h, err)
	}
	execute(t, s, "1", "honor-month00001", Command{Op: "title", TitleKey: "post-month"}, false)
	h, _ = s.Honors(ctx, "1", "", 1)
	if h.Selected == nil || h.Selected.Style != "glow" {
		t.Fatal(h)
	}
	*now = now.AddDate(0, 0, 8)
	h, _ = s.Honors(ctx, "1", "", 1)
	if len(h.Titles) != 1 || h.Badges[0].Count != 2 {
		t.Fatal(h)
	}
	if _, err = s.Execute(ctx, "1", "expired-choice01", Command{Op: "title", TitleKey: "post-week"}, false); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	execute(t, s, "1", "hide-title000001", Command{Op: "title"}, false)
	h, _ = s.Honors(ctx, "1", "", 1)
	if h.Selected != nil {
		t.Fatal(h)
	}
	execute(t, s, "3", "honor-revoke0001", reviewCommand("revoke", "post", "100", 3), true)
	h, _ = s.Honors(ctx, "1", "", 1)
	if len(h.Badges) != 0 || len(h.Titles) != 0 {
		t.Fatal("revoked grants remained", h)
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM metar_award_manual_payment").Scan(&count); err != nil || count != 1 {
		t.Fatal("payment history lost", err, count)
	}
}
