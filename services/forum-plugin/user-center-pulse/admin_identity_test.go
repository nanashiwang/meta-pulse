package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestMySQLAdminIdentityRequiresLiveActivatedAdministrator(t *testing.T) {
	dsn := requireDisposableForumDSN(t, "FORUM_BINDING_GUARD_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	exec("DROP TABLE IF EXISTS `user`")
	exec("DROP TABLE IF EXISTS user_role_rel")
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS user_role_rel")
		_, _ = db.Exec("DROP TABLE IF EXISTS `user`")
	})
	exec("CREATE TABLE `user` (id BIGINT NOT NULL PRIMARY KEY, status INT NOT NULL, mail_status INT NOT NULL)")
	exec("CREATE TABLE user_role_rel (id INT NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT NOT NULL, role_id INT NOT NULL)")
	exec("INSERT INTO `user` VALUES (7,1,1),(8,1,1),(9,1,1)")
	exec("INSERT INTO user_role_rel(user_id,role_id) VALUES (7,2),(8,3)")
	// No external binding table is needed: this admin represents Answer, not a
	// new-api funding identity. Every call checks current DB state.
	guard := &mysqlBindingGuard{db: db}
	if err := guard.CheckAdminIdentity(ctx, "7"); err != nil {
		t.Fatalf("unbound administrator rejected: %v", err)
	}
	for _, id := range []string{"8", "9", "10"} {
		if err := guard.CheckAdminIdentity(ctx, id); !errors.Is(err, errAdminForbidden) {
			t.Fatalf("non-admin %s accepted: %v", id, err)
		}
	}
	for _, mutation := range []string{
		"UPDATE user_role_rel SET role_id=3 WHERE user_id=7",
		"DELETE FROM user_role_rel WHERE user_id=7",
		"UPDATE `user` SET status=9 WHERE id=7",
		"UPDATE `user` SET status=10 WHERE id=7",
		"UPDATE `user` SET mail_status=2 WHERE id=7",
		"INSERT INTO user_role_rel(user_id,role_id) VALUES (7,2)",
	} {
		exec(mutation)
		if err := guard.CheckAdminIdentity(ctx, "7"); !errors.Is(err, errAdminForbidden) {
			t.Fatalf("cached admin survived live revocation %s: %v", mutation, err)
		}
		exec("UPDATE `user` SET status=1,mail_status=1 WHERE id=7")
		exec("DELETE FROM user_role_rel WHERE user_id=7")
		exec("INSERT INTO user_role_rel(user_id,role_id) VALUES (7,2)")
	}
	for _, id := range []string{"0", "07", "7 OR 1=1", ""} {
		if err := guard.CheckAdminIdentity(ctx, id); !errors.Is(err, errCommunityUnauthenticated) {
			t.Fatalf("invalid identity %q reached DB authorization: %v", id, err)
		}
	}
	exec("DROP TABLE user_role_rel")
	if err := guard.CheckAdminIdentity(ctx, "7"); err == nil {
		t.Fatal("unavailable role data failed open")
	}
}
