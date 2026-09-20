package growth

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
	dsn := os.Getenv("GROWTH_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("GROWTH_INTEGRATION_DSN required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("requires disposable _test database")
	}
	cfg.ParseTime = true
	// Fixture timestamps are generated in UTC. Keep the MySQL session in UTC
	// too; the server may default to Asia/Shanghai in CI or production.
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	for _, name := range []string{"metar_exp_config", "metar_exp_account", "metar_exp_event", "metar_exp_ledger", "metar_exp_cursor", "metar_exp_notice", "metar_exp_audit", "question", "answer", "activity", "config", "user_role_rel", "user"} {
		if _, err = db.Exec("DROP TABLE IF EXISTS `" + name + "`"); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"CREATE TABLE `user` (id BIGINT PRIMARY KEY,username VARCHAR(60),status INT,mail_status INT,created_at TIMESTAMP)",
		"CREATE TABLE user_role_rel (user_id BIGINT,role_id INT)",
		"CREATE TABLE question (id BIGINT PRIMARY KEY,user_id BIGINT,status INT,`show` INT,accepted_answer_id BIGINT DEFAULT 0,created_at TIMESTAMP)",
		"CREATE TABLE answer (id BIGINT PRIMARY KEY,user_id BIGINT,question_id BIGINT,status INT,created_at TIMESTAMP)",
		"CREATE TABLE config (id INT PRIMARY KEY,`key` VARCHAR(80))",
		"CREATE TABLE activity (id BIGINT PRIMARY KEY,user_id BIGINT,original_object_id BIGINT,trigger_user_id BIGINT,activity_type INT,cancelled INT DEFAULT 0,created_at TIMESTAMP)",
		"INSERT INTO `user` VALUES (1,'admin',1,1,'2026-01-01'),(2,'member',1,1,'2026-01-01'),(3,'voter',1,1,'2026-01-01'),(4,'inactive',1,2,'2026-01-01')",
		"INSERT INTO user_role_rel VALUES (1,2)",
		"INSERT INTO config VALUES (1,'answer.accepted'),(2,'answer.voted_up'),(3,'question.voted_up')",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	s := New(db)
	s.Now = func() time.Time { return now }
	if err = s.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, &now
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func balance(t *testing.T, s *Store, user string, want int64) {
	t.Helper()
	v, e := s.Summary(context.Background(), user)
	must(t, e)
	if v.Balance != want {
		t.Fatalf("balance %d want %d", v.Balance, want)
	}
	var ledger int64
	must(t, s.DB.QueryRow("SELECT COALESCE(SUM(delta),0) FROM metar_exp_ledger WHERE user_id=?", user).Scan(&ledger))
	if ledger != want {
		t.Fatalf("ledger %d want %d", ledger, want)
	}
}
func concurrent(t *testing.T, fn func() error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- fn() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
}
func TestMySQLCheckinAndPulseReplay(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	concurrent(t, func() error { return s.Checkin(ctx, "2") })
	balance(t, s, "2", 2)
	for i := 0; i < 6; i++ {
		*now = now.AddDate(0, 0, 1)
		must(t, s.Checkin(ctx, "2"))
	}
	balance(t, s, "2", 15)
	concurrent(t, func() error { return s.AwardPulse(ctx, "2", "test-grant", 600, false) })
	balance(t, s, "2", 615)
	if err := s.AwardPulse(ctx, "2", "test-grant", 601, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed award: %v", err)
	}
	concurrent(t, func() error { return s.AwardPulse(ctx, "2", "test-grant", 600, true) })
	balance(t, s, "2", 15)
	must(t, s.AwardPulse(ctx, "2", "test-grant", 600, false))
	balance(t, s, "2", 15)
	must(t, s.AwardPulse(ctx, "2", "reverse-first", 500, true))
	must(t, s.AwardPulse(ctx, "2", "reverse-first", 500, false))
	balance(t, s, "2", 15)
	if err := s.Checkin(ctx, "4"); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	*now = now.AddDate(0, 0, 2)
	must(t, s.Checkin(ctx, "2"))
	v, err := s.Summary(ctx, "2")
	must(t, err)
	if v.Streak != 1 {
		t.Fatal("broken streak")
	}
}
func TestMySQLCollectionCapsAndReversals(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		_, err := s.DB.Exec("INSERT INTO question(id,user_id,status,`show`,created_at) VALUES(?,2,1,1,?)", i, *now)
		must(t, err)
	}
	must(t, s.Collect(ctx))
	balance(t, s, "2", 0)
	pending, err := s.Pending(ctx, "2")
	must(t, err)
	if len(pending) != 3 {
		t.Fatal("observation")
	}
	rules := DefaultRules()
	rules.Question.Amount = 9
	rules.Question.DailyAmount = 9
	must(t, s.UpdateRules(ctx, "1", "rules-1", "调整测试", 1, rules))
	*now = now.Add(24 * time.Hour)
	for i := 0; i < 3; i++ {
		must(t, s.Collect(ctx))
	}
	balance(t, s, "2", 5)
	_, err = s.DB.Exec("UPDATE question SET status=10 WHERE id=1")
	must(t, err)
	for i := 0; i < 3; i++ {
		must(t, s.Collect(ctx))
	}
	balance(t, s, "2", 0)
	_, err = s.DB.Exec("UPDATE question SET status=1 WHERE id=1")
	must(t, err)
	for i := 0; i < 3; i++ {
		must(t, s.Collect(ctx))
	}
	balance(t, s, "2", 0)
	must(t, s.Feature(ctx, "1", "feature-1", "question", "2", "优质提问"))
	must(t, s.Feature(ctx, "1", "feature-2", "question", "2", "重复精选"))
	balance(t, s, "2", 50)
}
func TestMySQLGovernanceAndIdempotency(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if err := s.Adjust(ctx, "2", "denied", "2", "假冒管理员", 150); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	concurrent(t, func() error { return s.Adjust(ctx, "1", "same", "2", "迁移历史贡献", 1750) })
	balance(t, s, "2", 1750)
	if err := s.Adjust(ctx, "1", "same", "2", "迁移历史贡献", 1800); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	must(t, s.Appearance(ctx, "2", "frame"))
	if err := s.Appearance(ctx, "2", "sage"); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	must(t, s.Adjust(ctx, "1", "deduct", "2", "撤销误发", 1700*-1))
	v, err := s.Summary(ctx, "2")
	must(t, err)
	if v.Appearance != "default" || v.Level.Number != 0 {
		t.Fatal("downgrade appearance")
	}
	must(t, s.UpdateRules(ctx, "1", "rules", "调整", 1, DefaultRules()))
	must(t, s.UpdateRules(ctx, "1", "rules", "调整", 1, DefaultRules()))
	if err := s.UpdateRules(ctx, "1", "stale", "调整", 1, DefaultRules()); !errors.Is(err, ErrConflict) {
		t.Fatal("CAS", err)
	}
}
func TestMySQLVoteEligibilityAndAccepted(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	must(t, s.Adjust(ctx, "1", "voter-level", "3", "认可资格", 150))
	*now = now.Add(time.Second)
	_, err := s.DB.Exec("INSERT INTO question VALUES (1,2,1,1,0,?),(2,2,1,1,0,?),(3,2,1,1,0,?),(4,2,1,1,0,?)", *now, *now, *now, *now)
	must(t, err)
	for i := 1; i <= 4; i++ {
		_, err = s.DB.Exec("INSERT INTO activity VALUES (?,2,?,3,3,0,?)", i, i, *now)
		must(t, err)
	}
	_, err = s.DB.Exec("INSERT INTO answer VALUES (10,3,1,1,?)", *now)
	must(t, err)
	_, err = s.DB.Exec("UPDATE question SET accepted_answer_id=10 WHERE id=1")
	must(t, err)
	_, err = s.DB.Exec("INSERT INTO activity VALUES (10,3,10,2,1,0,?)", *now)
	must(t, err)
	must(t, s.Collect(ctx))
	*now = now.Add(24 * time.Hour)
	for i := 0; i < 3; i++ {
		must(t, s.Collect(ctx))
	}
	balance(t, s, "2", 11)
	balance(t, s, "3", 178)
	_, err = s.DB.Exec("UPDATE activity SET cancelled=1 WHERE id=1")
	must(t, err)
	_, err = s.DB.Exec("UPDATE question SET accepted_answer_id=0 WHERE id=1")
	must(t, err)
	for i := 0; i < 3; i++ {
		must(t, s.Collect(ctx))
	}
	balance(t, s, "2", 9)
	balance(t, s, "3", 158)
	_, err = s.DB.Exec("UPDATE activity SET cancelled=0 WHERE id=1")
	must(t, err)
	_, err = s.DB.Exec("UPDATE question SET accepted_answer_id=10 WHERE id=1")
	must(t, err)
	for i := 0; i < 3; i++ {
		must(t, s.Collect(ctx))
	}
	balance(t, s, "2", 9)
	balance(t, s, "3", 158)
	if _, err = s.DB.Exec("UPDATE metar_exp_ledger SET delta=999"); err == nil {
		t.Fatal("mutable ledger")
	}
	if _, err = s.DB.Exec("DELETE FROM metar_exp_audit"); err == nil {
		t.Fatal("mutable audit")
	}
	must(t, s.Ensure(ctx))
}
func TestMySQLConcurrentDailyCap(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.transaction(ctx, "2", func(tx *sql.Tx, a *Account) error {
				e, err := register(ctx, tx, Event{Source: fmt.Sprintf("cap-test:%d", i), UserID: "2", Kind: "answer", Day: Day(*now), OccurredAt: now.Unix(), EligibleAt: now.Unix(), Amount: 8, DailyCap: 60, DailyCount: 100, DailyAmount: 60, RuleVersion: 1, Reason: "并发限额测试"})
				if err != nil {
					return err
				}
				return settle(ctx, tx, a, e, *now)
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	balance(t, s, "2", 56)
	must(t, s.AwardPulse(ctx, "2", "outside-daily-limit", 150, false))
	balance(t, s, "2", 206)
}
