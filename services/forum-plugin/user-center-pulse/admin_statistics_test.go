package pulse_user_center

import (
	"context"
	"database/sql"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
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
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,user_id BIGINT,status INT)",
		"CREATE TABLE comment(id BIGINT PRIMARY KEY,user_id BIGINT,status INT)",
		"CREATE TABLE config(id INT PRIMARY KEY,`key` VARCHAR(64))",
		"CREATE TABLE activity(user_id BIGINT,trigger_user_id BIGINT,original_object_id BIGINT,activity_type INT,cancelled INT)",
		"CREATE TABLE metar_question_share(question_id BIGINT PRIMARY KEY,clicks BIGINT NOT NULL DEFAULT 0)",
		"INSERT INTO question VALUES(101,12,1,1),(102,12,2,1),(103,12,10,1)",
		"INSERT INTO answer VALUES(201,12,1),(202,12,10)",
		"INSERT INTO comment VALUES(301,12,1),(302,12,10)",
		"INSERT INTO config VALUES(1,'question.voted_up'),(2,'answer.voted_up'),(3,'question.voted_down')",
		"INSERT INTO activity VALUES(12,13,101,1,0),(12,13,101,1,1),(12,13,103,1,0),(13,12,201,2,0),(13,12,101,3,0)",
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
	if *got["12"] != (adminCommunityStatistics{Posts: 2, PostLikes: 1, Comments: 2, Likes: 1, Shares: 7}) || *got["14"] != (adminCommunityStatistics{}) {
		t.Fatalf("%+v %+v", got["12"], got["14"])
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
