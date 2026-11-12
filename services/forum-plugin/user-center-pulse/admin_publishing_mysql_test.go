package pulse_user_center

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
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
	for _, table := range []string{"metar_operator_publication", "metar_operator_account", "user_external_login", "user_role_rel", "user", "question", "answer"} {
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
	if _, err = db.Exec("UPDATE user_role_rel SET role_id=1 WHERE user_id=7"); err != nil {
		t.Fatal(err)
	}
	if w = publishingRequest(router, item, ""); w.Code != 403 {
		t.Fatal("demoted admin retained authority", w.Code)
	}
}
