package pulse_user_center

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestPublishingMySQLPersistence(t *testing.T) {
	dsn := os.Getenv("PUBLISHING_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("requires isolated _publishing_test database")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_publishing_test") {
		t.Fatal("requires isolated _publishing_test database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"metar_operator_plan", "metar_operator_publication", "metar_operator_account", "user_external_login", "user_role_rel", "user", "question", "answer"} {
		if _, err = db.Exec("DROP TABLE IF EXISTS `" + table + "`"); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"CREATE TABLE user(id BIGINT PRIMARY KEY,username VARCHAR(100),display_name VARCHAR(100),status INT,mail_status INT)",
		"CREATE TABLE user_role_rel(user_id BIGINT,role_id INT)",
		"CREATE TABLE user_external_login(user_id BIGINT,provider VARCHAR(100))",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,original_text TEXT)",
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,user_id BIGINT,original_text TEXT)",
		"INSERT INTO user VALUES(7,'admin','Admin',1,1),(23,'editor','Editor',1,1),(24,'bound','Bound',1,1)",
		"INSERT INTO user_role_rel VALUES(7,2),(23,1),(24,1)",
		"INSERT INTO user_external_login VALUES(24,'pulse_user_center')",
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	oldDB, oldEnabled := publishingDatabase, publishingEnabled
	publishingDatabase = func(context.Context) (*sql.DB, error) { return db, nil }
	publishingEnabled = func() bool { return true }
	uc := &UserCenter{Guard: &mysqlBindingGuard{db: db}}
	operatorPublisher.RLock()
	oldCall := operatorPublisher.call
	operatorPublisher.RUnlock()
	calls := 0
	uc.RegisterOperatorPublisher(func(c *gin.Context, user, kind string, body []byte) (int, []byte) {
		calls++
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		_, e := db.Exec("INSERT INTO question VALUES(123,?,?)", user, payload["content"])
		if e != nil {
			t.Fatal(e)
		}
		return 500, []byte("{}")
	})
	t.Cleanup(func() {
		publishingDatabase = oldDB
		publishingEnabled = oldEnabled
		uc.RegisterOperatorPublisher(oldCall)
	})
	router := gin.New()
	oldStart := publishingStartRunner
	publishingStartRunner = func(func()) {}
	t.Cleanup(func() { publishingStartRunner = oldStart })
	uc.registerPublishing(router.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
	enroll := communityTestRequest("POST", "x", `{"username":"editor","reason":"专用运营账号"}`)
	enroll.URL.Path = "/answer/admin/api/metar/publishing/accounts"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, enroll)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	item := validPublishingItem()
	w = publishingRequest(router, item, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "needs_review") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = publishingRequest(router, item, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "published") || calls != 1 {
		t.Fatal(w.Code, w.Body.String(), calls)
	}
	var actor, target int
	var content, status string
	if err = db.QueryRow("SELECT actor_id,target_id,payload,status FROM metar_operator_publication").Scan(&actor, &target, &content, &status); err != nil || actor != 7 || target != 23 || status != "published" || !strings.Contains(content, "社区运营") {
		t.Fatal(actor, target, status, err)
	}
	if _, err = operatorTarget(context.Background(), db, "bound", false); err == nil {
		t.Fatal("bound target accepted")
	}
	// Persisted scheduling, dependency order, concurrent workers and cancellation.
	var dispatch gin.HandlerFunc
	for _, route := range router.Routes() {
		if route.Path == "/answer/admin/api/metar/publishing/item" {
			dispatch = route.HandlerFunc
		}
	}
	if dispatch == nil {
		t.Fatal("missing dispatch")
	}
	planCalls := 0
	uc.RegisterOperatorPublisher(func(c *gin.Context, user, kind string, body []byte) (int, []byte) {
		planCalls++
		if kind == "comment" {
			return 200, []byte(fmt.Sprintf(`{"data":{"comment_id":"%d"}}`, 1000+planCalls))
		}
		return 200, []byte(fmt.Sprintf(`{"data":{"id":"%d"}}`, 1000+planCalls))
	})
	send := func(path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := communityTestRequest("POST", "x", string(raw))
		req.URL.Path = "/answer/admin/api/metar/publishing" + path
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	q := validPublishingItem()
	a := q
	a.Kind = "answer"
	comment := q
	comment.Kind = "comment"
	plan := publishingPlan{Key: "mysql-plan-123456789", Items: []publishingPlanItem{{publishingItem: q, ID: "q"}, {publishingItem: a, ID: "a", Parent: "q"}, {publishingItem: comment, ID: "c", Parent: "a"}, {publishingItem: comment, ID: "future", Parent: "c", PublishAt: "2099-01-01T00:00:00Z"}}}
	for i := 0; i < 2; i++ {
		if w := send("/plans", plan); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	changed := plan
	changed.Items = append([]publishingPlanItem(nil), plan.Items...)
	changed.Items[0].Content = "已改变的内容正文"
	if w := send("/plans", changed); w.Code != 409 {
		t.Fatal("missing conflict", w.Code, w.Body.String())
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); uc.runPublishingPlans(dispatch) }()
	}
	group.Wait()
	if planCalls != 3 {
		t.Fatal("duplicate or premature dispatch", planCalls)
	}
	var saved string
	var planStatus string
	if err := db.QueryRow("SELECT results,status FROM metar_operator_plan WHERE request_key=?", plan.Key).Scan(&saved, &planStatus); err != nil || planStatus != "queued" {
		t.Fatal(err, planStatus)
	}
	var results map[string]publishingPlanResult
	json.Unmarshal([]byte(saved), &results)
	if results["c"].ObjectID != "1003" {
		t.Fatal(results)
	}
	if w := send("/plans/cancel", map[string]string{"key": plan.Key}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	uc.runPublishingPlans(dispatch)
	if planCalls != 3 {
		t.Fatal("cancelled plan dispatched")
	}
	// Lost native response pauses the plan; explicit retry queries the old marker.
	uc.RegisterOperatorPublisher(func(c *gin.Context, user, kind string, body []byte) (int, []byte) {
		planCalls++
		var payload map[string]any
		json.Unmarshal(body, &payload)
		if _, err := db.Exec("INSERT INTO question VALUES(124,?,?)", user, payload["content"]); err != nil {
			t.Fatal(err)
		}
		return 500, []byte("{}")
	})
	plan.Key = "mysql-unknown-123456789"
	plan.Items = plan.Items[:1]
	if w := send("/plans", plan); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	uc.runPublishingPlans(dispatch)
	if err := db.QueryRow("SELECT status FROM metar_operator_plan WHERE request_key=?", plan.Key).Scan(&planStatus); err != nil || planStatus != "needs_review" {
		t.Fatal(err, planStatus)
	}
	if w := send("/plans/retry", map[string]string{"key": plan.Key}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	uc.runPublishingPlans(dispatch)
	if err := db.QueryRow("SELECT status FROM metar_operator_plan WHERE request_key=?", plan.Key).Scan(&planStatus); err != nil || planStatus != "completed" || planCalls != 4 {
		t.Fatal(err, planStatus, planCalls)
	}

	if _, err = db.Exec("UPDATE user_role_rel SET role_id=1 WHERE user_id=7"); err != nil {
		t.Fatal(err)
	}
	if w = publishingRequest(router, item, ""); w.Code != 403 {
		t.Fatal("demoted admin retained authority", w.Code)
	}
}
