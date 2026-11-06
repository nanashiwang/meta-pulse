package pulse_user_center

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMySQLAdminSwitch(t *testing.T) {
	dsn := requireDisposableForumDSN(t, "FORUM_SWITCH_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"metar_admin_switch", "user_external_login", "user_role_rel", "user"} {
		exec("DROP TABLE IF EXISTS `" + table + "`")
	}
	exec("CREATE TABLE `user` (id BIGINT PRIMARY KEY,username VARCHAR(100) UNIQUE,status INT,mail_status INT)")
	exec("CREATE TABLE user_role_rel (id BIGINT AUTO_INCREMENT PRIMARY KEY,user_id BIGINT,role_id INT,INDEX(user_id))")
	exec("CREATE TABLE user_external_login (id BIGINT AUTO_INCREMENT PRIMARY KEY,created_at TIMESTAMP,updated_at TIMESTAMP,user_id BIGINT,provider VARCHAR(100),external_id VARCHAR(128),meta_info TEXT,INDEX(provider,external_id))")
	if err = ensureSwitchTable(ctx, db); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO `user` VALUES(1,'admin',1,1),(2,'member',1,1),(3,'moderator',1,1),(4,'inactive',1,2),(5,'banned',9,1),(6,'deleted',10,1),(7,'admin2',1,1)")
	exec("INSERT INTO user_role_rel(user_id,role_id) VALUES(1,2),(3,3),(7,2)")
	issue := func() string {
		t.Helper()
		v, e := issueSwitch(ctx, db, "1", "member", "admin-session", 1000)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	for _, name := range []string{"admin", "admin2", "inactive", "banned", "deleted", "missing"} {
		if _, e := issueSwitch(ctx, db, "1", name, "admin-session", 1000); e == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, actor := range []string{"2", "3", "4", "5", "6", "01", "0"} {
		if _, e := issueSwitch(ctx, db, actor, "member", "session", 1000); e == nil {
			t.Fatalf("accepted actor %s", actor)
		}
	}
	ticket := issue()
	for _, proof := range []string{"", ticket + "x"} {
		if _, e := consumeSwitch(ctx, db, proof, "admin-session", 1001); e == nil {
			t.Fatal("bad proof")
		}
	}
	if _, e := consumeSwitch(ctx, db, ticket, "other-session", 1001); e == nil {
		t.Fatal("wrong session")
	}
	if _, e := consumeSwitch(ctx, db, ticket, "admin-session", 1060); e == nil {
		t.Fatal("expired ticket")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			external, e := consumeSwitch(ctx, db, ticket, "admin-session", 1001)
			if e == nil {
				if external != "admin-switch:2" {
					t.Error(external)
				}
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("replay successes %d", successes.Load())
	}
	var aliases, audits int
	if err = db.QueryRow("SELECT COUNT(*) FROM user_external_login WHERE provider=?", switchSlug).Scan(&aliases); err != nil || aliases != 1 {
		t.Fatalf("alias count %d %v", aliases, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM metar_admin_switch WHERE consumed_at IS NOT NULL").Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
	ticket = issue()
	exec("UPDATE user_role_rel SET role_id=3 WHERE user_id=1")
	if _, e := consumeSwitch(ctx, db, ticket, "admin-session", 1001); e == nil {
		t.Fatal("demoted administrator")
	}
	exec("UPDATE user_role_rel SET role_id=2 WHERE user_id=1")
	for _, mutation := range []string{"UPDATE `user` SET status=9 WHERE id=2", "UPDATE `user` SET mail_status=2 WHERE id=2", "INSERT INTO user_role_rel(user_id,role_id) VALUES(2,2)", "INSERT INTO user_role_rel(user_id,role_id) VALUES(2,1),(2,1)"} {
		exec(mutation)
		if _, e := consumeSwitch(ctx, db, ticket, "admin-session", 1001); e == nil {
			t.Fatal("unavailable target accepted")
		}
		exec("UPDATE `user` SET status=1,mail_status=1 WHERE id=2")
		exec("DELETE FROM user_role_rel WHERE user_id=2")
	}
	exec("UPDATE user_external_login SET user_id=3 WHERE provider='metar_admin_switch'")
	if _, e := consumeSwitch(ctx, db, ticket, "admin-session", 1001); e == nil {
		t.Fatal("alias mismatch")
	}
	exec("UPDATE user_external_login SET user_id=2 WHERE provider='metar_admin_switch'")
	if _, e := consumeSwitch(ctx, db, ticket, "admin-session", 1001); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		issue()
	}
	if _, e := issueSwitch(ctx, db, "1", "member", "admin-session", 1000); e == nil {
		t.Fatal("issuance rate unbounded")
	}
	if _, e := issueSwitch(ctx, db, "1", "moderator", "admin-session", 1061); e != nil {
		t.Fatal(e)
	}
}
