package forum

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestFetchMapsAnswerAuthorThroughGuardedBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := NewReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}
	query := regexp.QuoteMeta(`
SELECT q.id, binding.meta_pulse_external_id_guard, q.title,
       UNIX_TIMESTAMP(q.created_at), q.show, q.status
FROM (
  SELECT id, user_id, title, created_at, ` + "`show`" + `, status
  FROM question
  WHERE id > ?
  ORDER BY id ASC
  LIMIT ?
) AS q
LEFT JOIN user_external_login AS binding
  ON binding.meta_pulse_user_id_guard = q.user_id
 AND binding.created_at < q.created_at
ORDER BY q.id ASC`)
	mock.ExpectQuery(query).WithArgs(int64(40), 10).WillReturnRows(
		sqlmock.NewRows([]string{"id", "external_id", "title", "created_at_unix", "show", "status"}).
			AddRow(41, "9001", "绑定后的内容", 1700000000, 1, 1).
			AddRow(42, nil, "未绑定内容", 1700000001, 1, 1),
	)
	events, err := reader.Fetch(context.Background(), "40", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].AuthorUserID != 9001 || events[0].SourceContentID != "41" ||
		events[0].SkipCandidate || !events[1].SkipCandidate || events[1].SourceContentID != "42" {
		t.Fatalf("events=%+v", events)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFetchFailsClosedOnInvalidExternalIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, _ := NewReaderForDB(db)
	mock.ExpectQuery("SELECT q.id").WithArgs(int64(0), 1).WillReturnRows(
		sqlmock.NewRows([]string{"id", "external_id", "title", "created_at_unix", "show", "status"}).AddRow(1, "001", "unsafe", 1700000000, 1, 1),
	)
	if _, err := reader.Fetch(context.Background(), "", 1); err == nil {
		t.Fatal("non-canonical new-api identity was accepted")
	}
}

func TestContentReaderDisposableDSNRejectsBusinessSchemas(t *testing.T) {
	for _, tc := range []struct {
		dsn  string
		want bool
	}{
		{"root:secret@tcp(127.0.0.1:3306)/pulse_integration?parseTime=true", true},
		{"root:secret@tcp(127.0.0.1:3306)/forum-test?parseTime=true", true},
		{"root:secret@tcp(127.0.0.1:3306)/meta_pulse_forum?parseTime=true", false},
		{"invalid", false},
	} {
		if got := contentReaderDisposableDSN(tc.dsn); got != tc.want {
			t.Errorf("contentReaderDisposableDSN(%q)=%v want %v", tc.dsn, got, tc.want)
		}
	}
}

func contentReaderDisposableDSN(raw string) bool {
	cfg, err := mysqlDriver.ParseDSN(strings.TrimSpace(raw))
	if err != nil || cfg.DBName == "" {
		return false
	}
	name := strings.ToLower(cfg.DBName)
	return strings.HasSuffix(name, "_integration") || strings.HasSuffix(name, "-integration") ||
		strings.HasSuffix(name, "_test") || strings.HasSuffix(name, "-test")
}

func requireContentReaderDisposableDSN(t *testing.T, envName string) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(envName))
	if dsn == "" {
		t.Skip(envName + " is not set")
	}
	if !contentReaderDisposableDSN(dsn) {
		t.Fatalf("%s must select a disposable schema ending in _integration, -integration, _test, or -test", envName)
	}
	return dsn
}

func TestMySQLFetchUsesAnswerV171SchemaAndBindingTime(t *testing.T) {
	dsn := requireContentReaderDisposableDSN(t, "FORUM_CONTENT_READER_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, table := range []string{"question", "user_external_login"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS question")
		_, _ = db.Exec("DROP TABLE IF EXISTS user_external_login")
	})
	statements := []string{
		`CREATE TABLE user_external_login (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			user_id BIGINT NOT NULL DEFAULT 0,
			provider VARCHAR(100) NOT NULL DEFAULT '',
			external_id VARCHAR(128) NOT NULL DEFAULT '',
			meta_pulse_external_id_guard VARCHAR(128) GENERATED ALWAYS AS (IF(provider = 'pulse_user_center', external_id, NULL)) STORED,
			meta_pulse_user_id_guard BIGINT GENERATED ALWAYS AS (IF(provider = 'pulse_user_center', user_id, NULL)) STORED,
			UNIQUE KEY ux_meta_pulse_external_id (meta_pulse_external_id_guard),
			UNIQUE KEY ux_meta_pulse_user_id (meta_pulse_user_id_guard)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE question (
			id BIGINT NOT NULL PRIMARY KEY,
			created_at TIMESTAMP NOT NULL,
			user_id BIGINT NOT NULL,
			title VARCHAR(150) NOT NULL DEFAULT '',
			` + "`show`" + ` INT NOT NULL DEFAULT 1,
			status INT NOT NULL DEFAULT 1
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`INSERT INTO user_external_login(created_at, user_id, provider, external_id) VALUES
			('2026-01-02 00:00:00', 10, 'pulse_user_center', '101')`,
		`INSERT INTO question(id, created_at, user_id, title, ` + "`show`" + `, status) VALUES
			(1, '2026-01-01 00:00:00', 10, '绑定前', 1, 1),
			(2, '2026-01-03 00:00:00', 10, '绑定后', 1, 1),
			(3, '2026-01-04 00:00:00', 20, '未绑定', 1, 1),
			(4, '2026-01-05 00:00:00', 10, '隐藏', 2, 1),
			(5, '2026-01-06 00:00:00', 10, '待审核', 1, 11),
			(6, '2026-01-07 00:00:00', 10, '已关闭但公开', 1, 2),
			(7, '2026-01-02 00:00:00', 10, '与绑定同秒', 1, 1)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := NewReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}
	events, err := reader.Fetch(ctx, "0", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 7 {
		t.Fatalf("events=%+v", events)
	}
	wantSkipped := map[string]bool{"1": true, "3": true, "4": true, "5": true, "7": true}
	for _, event := range events {
		if event.SkipCandidate != wantSkipped[event.SourceContentID] {
			t.Fatalf("unexpected eligibility for event %+v", event)
		}
		if !event.SkipCandidate && event.AuthorUserID != 101 {
			t.Fatalf("Answer local id leaked as new-api identity: %+v", event)
		}
	}
}
