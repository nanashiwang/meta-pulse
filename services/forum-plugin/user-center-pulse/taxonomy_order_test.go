package pulse_user_center

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTaxonomyOrderValidation(t *testing.T) {
	for _, raw := range []string{`{"version":0,"categories":[],"tags":[]}`, `{"version":0,"categories":["开发","vibe-coding"],"tags":["ai"]}`} {
		var v taxonomyOrder
		if json.Unmarshal([]byte(raw), &v) != nil || !v.valid() {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"version":-1,"categories":[],"tags":[]}`, `{"version":0,"categories":["a","a"],"tags":[]}`, `{"version":0,"categories":[],"tags":null}`, `{"version":0,"categories":[],"tags":[""]}`} {
		var v taxonomyOrder
		_ = json.Unmarshal([]byte(raw), &v)
		if v.valid() {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"version":0,"version":1,"categories":[],"tags":[]}`, `{"version":0,"categories":[],"tags":[],"user_id":"1"}`, `{} {}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("PUT", "/", strings.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		if taxonomyJSON(c, new(taxonomyOrder)) {
			t.Fatal(raw)
		}
	}
}

func TestMySQLTaxonomyOrder(t *testing.T) {
	dsn := requireDisposableForumDSN(t, "FORUM_BINDING_GUARD_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string) {
		t.Helper()
		if _, e := db.Exec(query); e != nil {
			t.Fatal(e)
		}
	}
	exec("DROP TABLE IF EXISTS metar_taxonomy_order,user_role_rel,`user`")
	exec("CREATE TABLE `user` (id BIGINT PRIMARY KEY,status INT,mail_status INT)")
	exec("CREATE TABLE user_role_rel (user_id BIGINT,role_id INT)")
	exec("INSERT INTO `user` VALUES (7,1,1),(8,1,1)")
	exec("INSERT INTO user_role_rel VALUES (7,2),(8,3)")
	exec(`CREATE TABLE metar_taxonomy_order (id INT PRIMARY KEY, version BIGINT NOT NULL,payload MEDIUMTEXT NOT NULL) ENGINE=InnoDB`)
	exec(`INSERT INTO metar_taxonomy_order VALUES (1,0,'{"categories":[],"tags":[]}')`)
	taxonomyDB.Lock()
	previous := taxonomyDB.db
	taxonomyDB.db = db
	taxonomyDB.Unlock()
	defer func() { taxonomyDB.Lock(); taxonomyDB.db = previous; taxonomyDB.Unlock() }()
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS metar_taxonomy_order,user_role_rel,`user`") })
	uc := &UserCenter{}
	request := func(user, origin, raw string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "https://community.test/order", strings.NewReader(raw))
		c.Request.Header.Set("Origin", origin)
		c.Request.Header.Set("Authorization", "Bearer test")
		c.Request.Header.Set("X-Metar-Request", "1")
		c.Request.Header.Set("Content-Type", "application/json")
		uc.taxonomyOrderHandler(func(*gin.Context) (string, error) { return user, nil }, func() string { return "https://community.test" })(c)
		return w
	}
	body := `{"version":0,"categories":["b","a"],"tags":["z","x"]}`
	for _, pair := range [][2]string{{"8", "https://community.test"}, {"7", "https://evil.test"}} {
		if w := request(pair[0], pair[1], body); w.Code != 403 {
			t.Fatalf("unauthorized %d %s", w.Code, w.Body)
		}
	}
	var wg sync.WaitGroup
	codes := make(chan int, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- request("7", "https://community.test", body).Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("replay %d", code)
		}
	}
	saved, e := readTaxonomyOrder(context.Background(), db)
	if e != nil || saved.Version != 1 {
		t.Fatalf("saved %+v %v", saved, e)
	}
	if w := request("7", "https://community.test", strings.Replace(body, `"b","a"`, `"a","b"`, 1)); w.Code != 409 {
		t.Fatalf("conflict %d", w.Code)
	}
	var wg2 sync.WaitGroup
	codes2 := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg2.Add(1)
		go func(i int) {
			defer wg2.Done()
			codes2 <- request("7", "https://community.test", fmt.Sprintf(`{"version":1,"categories":[],"tags":["tag%d"]}`, i)).Code
		}(i)
	}
	wg2.Wait()
	close(codes2)
	counts := map[int]int{}
	for code := range codes2 {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	exec("UPDATE `user` SET status=9 WHERE id=7")
	if w := request("7", "https://community.test", body); w.Code != 403 {
		t.Fatal(w.Code)
	}
}
