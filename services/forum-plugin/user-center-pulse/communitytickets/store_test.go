package communitytickets

import (
	"context"
	"database/sql"
	mysql "github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEntitlements(t *testing.T) {
	for _, v := range []struct {
		l, c, r         int
		check           bool
		p, received, ci int
	}{{39, 19, 9, false, 0, 0, 0}, {40, 20, 10, true, 2, 1, 1}, {20, 10, 0, false, 0, 0, 0}, {120, 60, 30, true, 3, 2, 1}} {
		got := Entitlements(v.l, v.c, v.r, v.check)
		if got["participation"] != v.p || got["received"] != v.received || got["checkin"] != v.ci {
			t.Fatal(got, v)
		}
	}
	if Day(time.Date(2026, 11, 6, 16, 0, 0, 0, time.UTC)) != "2026-11-07" {
		t.Fatal("Shanghai midnight")
	}
}
func fixture(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	dsn := os.Getenv("TICKETS_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("TICKETS_INTEGRATION_DSN required")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasSuffix(cfg.DBName, "_tickets_test") {
		t.Fatal("requires isolated _tickets_test database")
	}
	cfg.ParseTime = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	for _, table := range []string{"metar_ticket_config", "metar_ticket_cursor", "metar_ticket_source", "metar_ticket_day", "metar_ticket_outbox", "metar_exp_event", "user_external_login", "comment", "answer", "question", "activity", "config", "user"} {
		must(t, db, "DROP TABLE IF EXISTS `"+table+"`")
	}
	for _, q := range []string{
		"CREATE TABLE `user`(id BIGINT PRIMARY KEY,status INT,mail_status INT)",
		"CREATE TABLE user_external_login(user_id BIGINT,external_id VARCHAR(32),provider VARCHAR(32),created_at TIMESTAMP)",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,`show` INT,created_at TIMESTAMP)",
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,user_id BIGINT,question_id BIGINT,status INT,created_at TIMESTAMP)",
		"CREATE TABLE comment(id BIGINT PRIMARY KEY,user_id BIGINT,question_id BIGINT,object_id BIGINT,status INT,created_at TIMESTAMP)",
		"CREATE TABLE activity(id BIGINT PRIMARY KEY,user_id BIGINT,trigger_user_id BIGINT,original_object_id BIGINT,activity_type INT,cancelled INT,created_at TIMESTAMP)",
		"CREATE TABLE config(id INT PRIMARY KEY,`key` VARCHAR(80))",
		"CREATE TABLE metar_exp_event(id BIGINT PRIMARY KEY,user_id BIGINT,kind VARCHAR(20),occurred_at BIGINT)",
		"INSERT INTO `user` VALUES(1,1,1),(2,1,1),(3,1,1),(4,10,1)",
		"INSERT INTO user_external_login VALUES(1,'101','pulse_user_center','2026-11-05'),(2,'102','pulse_user_center','2026-11-05'),(4,'104','pulse_user_center','2026-11-05')",
		"INSERT INTO config VALUES(1,'question.voted_up'),(2,'answer.voted_up'),(3,'answer.accepted')",
		"INSERT INTO question VALUES(1,1,1,1,'2026-11-06'),(2,2,1,1,'2026-11-06'),(3,1,10,1,'2026-11-06'),(4,1,1,1,'2026-11-04')",
	} {
		must(t, db, q)
	}
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	s := New(db)
	s.Now = func() time.Time { return now }
	if e = s.Ensure(context.Background()); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	return s, &now
}
func must(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, e := db.Exec(q, args...); e != nil {
		t.Fatal(e)
	}
}
func collect(t *testing.T, s *Store) {
	t.Helper()
	if e := s.Collect(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestMySQLTicketsSourcesCapsReplaysAndMidnight(t *testing.T) {
	s, now := fixture(t)
	db := s.DB
	ctx := context.Background()
	for i := 1; i <= 60; i++ {
		must(t, db, "INSERT INTO comment VALUES(?,2,1,1,1,?)", i, *now)
	}
	for i := 1; i <= 120; i++ {
		must(t, db, "INSERT INTO question VALUES(?,1,1,1,?)", i+100, *now)
		must(t, db, "INSERT INTO activity VALUES(?,1,2,?,1,0,?)", i, i+100, *now)
	}
	must(t, db, "INSERT INTO metar_exp_event VALUES(1,2,'checkin',?)", now.Unix())
	collect(t, s)
	p, e := s.Progress(ctx, "2")
	if e != nil || p.Earned != 4 || p.Comments != 60 || p.Likes != 120 || !p.CheckedIn {
		t.Fatalf("progress %+v %v", p, e)
	}
	r, e := s.Progress(ctx, "1")
	if e != nil || r.Earned != 2 || r.Received != 60 {
		t.Fatalf("received %+v %v", r, e)
	}
	// Simulate cursor/HTTP response loss. Neither rerunning source ingestion nor
	// retrying acknowledgement can create a second daily entitlement.
	for i := 0; i < 2; i++ {
		must(t, db, "UPDATE metar_ticket_cursor SET last_id=0")
		collect(t, s)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.record(ctx, source{ID: 1, Actor: "2", Owner: "1", At: now.Unix(), PostAt: now.Add(-time.Hour).Unix(), Valid: true}, "comment", "comment", "2")
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	rows, e := s.Pending(ctx)
	if e != nil || len(rows) != 6 {
		t.Fatalf("pending %d %v", len(rows), e)
	}
	for _, r := range rows {
		if e = s.Ack(ctx, r.ID); e != nil {
			t.Fatal(e)
		}
		if e = s.Ack(ctx, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	*now = now.Add(24 * time.Hour)
	// Re-liking the same object on a new day never earns again.
	for i := 1; i <= 40; i++ {
		must(t, db, "INSERT INTO activity VALUES(?,1,2,?,1,0,?)", i+200, i+100, *now)
	}
	must(t, db, "INSERT INTO metar_exp_event VALUES(2,2,'checkin',?)", now.Unix())
	collect(t, s)
	p, e = s.Progress(ctx, "2")
	if e != nil || p.Earned != 1 || p.Likes != 0 || p.Comments != 0 {
		t.Fatalf("midnight %+v %v", p, e)
	}
}
func TestMySQLTicketsInvalidSourcesAndSeparateThresholds(t *testing.T) {
	s, now := fixture(t)
	db := s.DB
	// Invalid self replies, deleted/hidden posts, prebinding posts, unbound and
	// banned actors must not create received entitlements.
	for i := 1; i <= 10; i++ {
		must(t, db, "INSERT INTO comment VALUES(?,1,1,1,1,?)", i, *now)
		must(t, db, "INSERT INTO comment VALUES(?,2,3,3,1,?)", i+10, *now)
		must(t, db, "INSERT INTO comment VALUES(?,2,4,4,1,?)", i+20, *now)
		must(t, db, "INSERT INTO comment VALUES(?,4,1,1,1,?)", i+30, *now)
	}
	collect(t, s)
	p, e := s.Progress(context.Background(), "1")
	if e != nil || p.Earned != 0 {
		t.Fatalf("invalid received %+v %v", p, e)
	}
	for i := 1; i <= 20; i++ {
		must(t, db, "INSERT INTO question VALUES(?,1,1,1,?)", i+100, *now)
		must(t, db, "INSERT INTO activity VALUES(?,1,2,?,1,0,?)", i, i+100, *now)
	}
	// Ten outgoing comments on another's older post were valid, but 20 likes +
	// ten comments must remain zero participation tickets (no mixed fractions).
	collect(t, s)
	p, e = s.Progress(context.Background(), "2")
	if e != nil || p.Earned != 0 || p.Comments != 10 {
		t.Fatalf("separate floors %+v %v", p, e)
	}
	for i := 21; i <= 40; i++ {
		must(t, db, "INSERT INTO question VALUES(?,1,1,1,?)", i+100, *now)
		must(t, db, "INSERT INTO activity VALUES(?,1,2,?,1,0,?)", i, i+100, *now)
	}
	must(t, db, "UPDATE activity SET cancelled=1 WHERE id=1")
	collect(t, s)
	p, e = s.Progress(context.Background(), "2")
	if e != nil || p.Likes != 39 || p.Earned != 0 {
		t.Fatalf("cancelled %+v %v", p, e)
	}
	// Real answers count as comments. All old receipts survive a new Store.
	for i := 1; i <= 10; i++ {
		must(t, db, "INSERT INTO answer VALUES(?,2,1,1,?)", i, *now)
	}
	collect(t, s)
	p, e = s.Progress(context.Background(), "2")
	if e != nil || p.Earned != 1 || p.Comments != 20 {
		t.Fatalf("answers %+v %v", p, e)
	}
	newer := New(db)
	newer.Now = s.Now
	if e = newer.Ensure(context.Background()); e != nil {
		t.Fatal(e)
	}
	collect(t, newer)
	rows, e := newer.Pending(context.Background())
	if e != nil || len(rows) != 2 {
		t.Fatalf("restart %+v %v", rows, e)
	}
}

func TestMySQLTicketsRollbackAndActivationBoundary(t *testing.T) {
	s, now := fixture(t)
	db := s.DB
	ctx := context.Background()
	must(t, db, "INSERT INTO metar_exp_event VALUES(1,2,'checkin',?)", now.Add(-2*time.Hour).Unix()) // before launch
	must(t, db, "INSERT INTO metar_exp_event VALUES(2,3,'checkin',?)", now.Unix())                   // unbound
	must(t, db, "INSERT INTO metar_exp_event VALUES(3,4,'checkin',?)", now.Unix())                   // banned
	collect(t, s)
	rows, e := s.Pending(ctx)
	if e != nil || len(rows) != 0 {
		t.Fatal("unqualified checkins", rows, e)
	}
	must(t, db, "CREATE TRIGGER test_outbox_failure BEFORE INSERT ON metar_ticket_outbox FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test interruption'")
	t.Cleanup(func() { db.Exec("DROP TRIGGER IF EXISTS test_outbox_failure") })
	must(t, db, "INSERT INTO metar_exp_event VALUES(4,2,'checkin',?)", now.Unix())
	if e = s.Collect(ctx); e == nil {
		t.Fatal("expected injected failure")
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM metar_ticket_source").Scan(&n)
	if n != 0 {
		t.Fatal("partial source committed", n)
	}
	must(t, db, "DROP TRIGGER test_outbox_failure")
	collect(t, s)
	rows, e = s.Pending(ctx)
	if e != nil || len(rows) != 1 {
		t.Fatal("lost retry", rows, e)
	}
	if e = s.Defer(ctx, rows[0].ID); e != nil {
		t.Fatal(e)
	}
	wait, e := s.Pending(ctx)
	if e != nil || len(wait) != 0 {
		t.Fatal("retry schedule", wait, e)
	}
	*now = now.Add(2 * time.Minute)
	wait, e = s.Pending(ctx)
	if e != nil || len(wait) != 1 {
		t.Fatal("durable retry lost", wait, e)
	}
}
