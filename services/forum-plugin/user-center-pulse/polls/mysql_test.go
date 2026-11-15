package polls

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

func fixture(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("POLLS_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("POLLS_INTEGRATION_DSN required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("disposable _test database required")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(16)
	for _, table := range []string{"metar_topic_poll_command", "metar_topic_poll_ballot", "metar_topic_poll", "question", "user"} {
		if _, err = db.Exec("DROP TABLE IF EXISTS " + "`" + table + "`"); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"CREATE TABLE `user`(id BIGINT PRIMARY KEY,status INT,mail_status INT) ENGINE=InnoDB", "INSERT INTO `user` VALUES(1,1,1),(2,1,1),(3,1,1),(4,1,2),(5,2,1)", "CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,`show` INT) ENGINE=InnoDB", "INSERT INTO question VALUES(100,1,1,1),(101,2,1,1),(102,1,11,1)"} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db)
	if err = s.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Unix(1800000000, 0) }
	return s
}
func TestPollMySQL(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	create := Command{Op: "create", QuestionID: "100", Config: config()}
	if _, err := s.Execute(ctx, "2", "create-wrong-owner", create); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, "1", "create-request-001", create); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, "1", "create-request-001", create); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, "1", "create-request-002", create); err != ErrConflict {
		t.Fatal(err)
	}
	public, err := s.Get(ctx, "100", "")
	if err != nil || public.ResultsVisible || public.Total != nil || public.Counts != nil {
		t.Fatalf("leaked results %+v %v", public, err)
	}
	for _, u := range []string{"4", "5"} {
		if _, err = s.Execute(ctx, u, "invalid-user-vote", Command{Op: "vote", QuestionID: "100", Choices: []int{0}}); err != ErrForbidden {
			t.Fatal(err)
		}
	}
	if _, err = s.Execute(ctx, "2", "too-many-choices1", Command{Op: "vote", QuestionID: "100", Choices: []int{0, 1, 2}}); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, "2", "invalid-choice-01", Command{Op: "vote", QuestionID: "100", Choices: []int{5}}); err != ErrInvalid {
		t.Fatal(err)
	}
	vote := Command{Op: "vote", QuestionID: "100", Choices: []int{0, 2}}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Execute(ctx, "2", "same-vote-request", vote); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	view, err := s.Get(ctx, "100", "2")
	if err != nil || !view.ResultsVisible || *view.Total != 1 || view.Counts[0] != 1 || view.Counts[2] != 1 || view.Version != 1 {
		t.Fatalf("view %+v %v", view, err)
	}
	if _, err = s.Execute(ctx, "2", "old-version-vote", Command{Op: "vote", QuestionID: "100", Choices: []int{1}, Version: 0}); err != ErrConflict {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, "2", "change-vote-00001", Command{Op: "vote", QuestionID: "100", Choices: []int{1}, Version: 1}); err != nil {
		t.Fatal(err)
	}
	view, err = s.Get(ctx, "100", "2")
	if err != nil || *view.Total != 1 || view.Counts[0] != 0 || view.Counts[2] != 0 || view.Counts[1] != 1 {
		t.Fatalf("replacement %+v %v", view, err)
	}
	if _, err = s.Execute(ctx, "2", "same-vote-request", vote); err != nil {
		t.Fatal("replay must not overwrite newer vote", err)
	}
	view, _ = s.Get(ctx, "100", "2")
	if view.Version != 2 || view.Counts[1] != 1 {
		t.Fatal(view)
	}
	if _, err = s.Execute(ctx, "3", "close-not-owner-01", Command{Op: "close", QuestionID: "100"}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, "1", "close-author-0001", Command{Op: "close", QuestionID: "100"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, "3", "vote-after-close1", Command{Op: "vote", QuestionID: "100", Choices: []int{0}}); err != ErrClosed {
		t.Fatal(err)
	}
	public, err = s.Get(ctx, "100", "")
	if err != nil || !public.EarlyClosed || !public.ResultsVisible || *public.Total != 1 {
		t.Fatal(public, err)
	}
	if _, err = s.DB.Exec("UPDATE question SET `show`=2 WHERE id=100"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, "100", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("hidden content", err)
	}
	if _, err = s.Execute(ctx, "1", "create-pending-01", Command{Op: "create", QuestionID: "102", Config: config()}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, "102", ""); err != ErrNotFound {
		t.Fatal(err)
	}
}
func TestPollConcurrentChangeAndDeadlineMySQL(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	c := Command{Op: "create", QuestionID: "100", Config: config()}
	if _, err := s.Execute(ctx, "1", "create-request-001", c); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Execute(ctx, "2", fmt.Sprintf("race-vote-%016d", i), Command{Op: "vote", QuestionID: "100", Choices: []int{i % 3}, Version: 0})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else if err != ErrConflict {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	s.Now = func() time.Time { return time.Unix(1800000000+7*86400, 0) }
	if _, err := s.Execute(ctx, "3", "vote-at-deadline1", Command{Op: "vote", QuestionID: "100", Choices: []int{0}}); err != ErrClosed {
		t.Fatal(err)
	}
	v, err := s.Get(ctx, "100", "")
	if err != nil || !v.Ended || !v.ResultsVisible {
		t.Fatal(v, err)
	}
}
