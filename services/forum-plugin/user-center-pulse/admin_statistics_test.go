package pulse_user_center

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMySQLAdminCommunityStatistics(t *testing.T) {
	dsn := requireDisposableForumDSN(t, "FORUM_STATISTICS_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	statements := []string{
		"DROP TABLE IF EXISTS question,answer,comment,activity,config,metar_question_share",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,`show` INT)",
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,created_at TIMESTAMP NULL)",
		"CREATE TABLE comment(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,created_at TIMESTAMP NULL)",
		"CREATE TABLE config(id INT PRIMARY KEY,`key` VARCHAR(64))",
		"CREATE TABLE activity(user_id BIGINT,trigger_user_id BIGINT,original_object_id BIGINT,activity_type INT,cancelled INT,created_at TIMESTAMP NULL)",
		"CREATE TABLE metar_question_share(question_id BIGINT PRIMARY KEY,clicks BIGINT NOT NULL DEFAULT 0)",
		"INSERT INTO question VALUES(101,12,1,1),(102,12,2,1),(103,12,10,1)",
		"INSERT INTO answer(id,user_id,status) VALUES(201,12,1),(202,12,10)",
		"INSERT INTO comment(id,user_id,status) VALUES(301,12,1),(302,12,10)",
		"INSERT INTO config VALUES(1,'question.voted_up'),(2,'answer.voted_up'),(3,'question.voted_down')",
		"INSERT INTO activity(user_id,trigger_user_id,original_object_id,activity_type,cancelled) VALUES(12,13,101,1,0),(12,13,101,1,1),(12,13,103,1,0),(13,12,201,2,0),(13,12,101,3,0)",
		"INSERT INTO metar_question_share VALUES(101,7),(103,99)",
	}
	for _, q := range statements {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	g := &mysqlBindingGuard{db: db}
	got, err := g.adminCommunityStatistics(context.Background(), []string{"12", "14"})
	if err != nil {
		t.Fatal(err)
	}
	for _, stat := range got {
		stat.RangeStart = ""
		stat.RangeEnd = ""
	}
	if *got["12"] != (adminCommunityStatistics{Posts: 2, PostLikes: 1, Comments: 2, Likes: 1, Shares: 7}) || *got["14"] != (adminCommunityStatistics{}) {
		t.Fatalf("%+v %+v", got["12"], got["14"])
	}

	now := time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC)
	start := beijingStatisticsStart(now).Unix()
	if _, err = db.Exec("INSERT INTO config VALUES(4,'comment.voted_up')"); err != nil {
		t.Fatal(err)
	}
	for i, sec := range []int64{start - 1, start, start + 1, now.Unix() + 1} {
		if _, err = db.Exec("INSERT INTO comment VALUES(?,12,1,FROM_UNIXTIME(?))", 400+i, sec); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO answer VALUES(?,12,1,FROM_UNIXTIME(?))", 400+i, sec); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO activity VALUES(13,12,301,4,0,FROM_UNIXTIME(?))", sec); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO comment VALUES(500,12,10,FROM_UNIXTIME(?)),(501,12,11,FROM_UNIXTIME(?))", start, start); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO activity VALUES(13,12,301,4,1,FROM_UNIXTIME(?)),(12,13,301,4,0,FROM_UNIXTIME(?))", start, start); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO answer VALUES(500,12,10,FROM_UNIXTIME(?)),(501,12,11,FROM_UNIXTIME(?)),(502,13,1,FROM_UNIXTIME(?))", start, start, start); err != nil {
		t.Fatal(err)
	}
	got, err = g.adminCommunityStatisticsAt(context.Background(), []string{"12", "13", "14"}, now)
	if err != nil || got["12"].RangeComments != 4 || got["12"].RangeLikes != 2 || got["13"].RangeComments != 1 || got["14"].RangeComments != 0 || got["14"].RangeLikes != 0 {
		t.Fatalf("daily %+v err=%v", got, err)
	}
	from, to, err := parseStatisticsRange("2026-11-16", "2026-11-16", now)
	if err != nil {
		t.Fatal(err)
	}
	got, err = g.adminCommunityStatisticsRange(context.Background(), []string{"12"}, from, to, now)
	if err != nil || got["12"].RangeComments != 2 || got["12"].RangeLikes != 1 || got["12"].RangeEnd != "2026-11-16" {
		t.Fatal(got, err)
	}

}

func TestMySQLShareStatisticsCountsClicksOnly(t *testing.T) {
	dsn := requireDisposableForumDSN(t, "FORUM_STATISTICS_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS question(id BIGINT PRIMARY KEY,user_id BIGINT,status INT,`show` INT)",
		"CREATE TABLE IF NOT EXISTS metar_question_share(question_id BIGINT PRIMARY KEY,clicks BIGINT NOT NULL DEFAULT 0)",
		"REPLACE INTO question VALUES(10010000000000101,12,1,1),(10010000000000102,12,10,1)",
		"DELETE FROM metar_question_share WHERE question_id IN(10010000000000101,10010000000000102)",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old := ticketGrantEnabled
	ticketGrantEnabled = func() bool { return true }
	defer func() { ticketGrantEnabled = old }()
	uc := &UserCenter{Guard: &mysqlBindingGuard{db: db}}
	r := gin.New()
	uc.registerShareStatistics(r.Group("/answer/api/v1"), func() string { return "https://community.test" })
	for _, id := range []string{"10010000000000101", "10010000000000101", "10010000000000102"} {
		req := httptest.NewRequest("POST", "/answer/api/v1/metar/statistics/share", strings.NewReader(`{"question_id":"`+id+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Origin", "https://community.test")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 204 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	var clicks int64
	if err = db.QueryRow("SELECT SUM(clicks) FROM metar_question_share WHERE question_id IN(10010000000000101,10010000000000102)").Scan(&clicks); err != nil || clicks != 2 {
		t.Fatalf("clicks=%d err=%v", clicks, err)
	}
	req := httptest.NewRequest("POST", "/answer/api/v1/metar/statistics/share", strings.NewReader(`{"question_id":"10010000000000101"}`))
	req.Header.Set("X-Metar-Request", "1")
	req.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestBeijingStatisticsDayBoundary(t *testing.T) {
	for _, value := range []struct{ now, want string }{{"2026-11-16T15:59:59Z", "2026-11-15T16:00:00Z"}, {"2026-11-16T16:00:00Z", "2026-11-16T16:00:00Z"}} {
		now, _ := time.Parse(time.RFC3339, value.now)
		if got := beijingStatisticsStart(now).UTC().Format(time.RFC3339); got != value.want {
			t.Fatal(got, value.want)
		}
	}
}

func TestStatisticsCustomDateRange(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	from, to, err := parseStatisticsRange("2026-11-01", "2026-11-16", now)
	if err != nil || from.UTC().Format(time.RFC3339) != "2026-10-31T16:00:00Z" || to.UTC().Format(time.RFC3339) != "2026-11-16T16:00:00Z" {
		t.Fatal(from, to, err)
	}
	for _, pair := range [][2]string{{"2026-11-17", "2026-11-01"}, {"2026-02-30", "2026-11-01"}, {"2026-11-18", "2026-11-18"}, {"", "2026-11-17"}, {"2026-11-01", ""}} {
		if _, _, err = parseStatisticsRange(pair[0], pair[1], now); err == nil {
			t.Fatal(pair)
		}
	}
	from, to, err = parseStatisticsRange("", "", now)
	if err != nil || from.Format("2006-01-02") != "2026-11-17" || to.Sub(from) != 24*time.Hour {
		t.Fatal(from, to, err)
	}
}
