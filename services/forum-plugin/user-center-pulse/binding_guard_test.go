package pulse_user_center

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestBindingGuardDefinitionValidationRejectsLookalikes(t *testing.T) {
	for _, expression := range []string{
		"if((`provider` = _utf8mb4'pulse_user_center'),`external_id`,NULL)",
		`if((` + "`provider`" + ` = _utf8mb4\'pulse_user_center\'),` + "`external_id`" + `,NULL))`,
	} {
		if !validGuardColumn("varchar", "varchar(128)", "STORED GENERATED", expression, "external_id") {
			t.Fatalf("valid MySQL generated expression rejected: %q", expression)
		}
	}
	if validGuardColumn("varchar", "varchar(128)", "STORED GENERATED", "if(provider='other',external_id,NULL)", "external_id") {
		t.Fatal("foreign provider expression accepted")
	}
	if validGuardColumn("varchar", "varchar(128)", "VIRTUAL GENERATED", "if(provider='pulse_user_center',external_id,NULL)", "external_id") {
		t.Fatal("virtual guard column accepted")
	}
	for event, statement := range map[string]string{
		"INSERT": insertTriggerDDL(),
		"UPDATE": updateTriggerDDL(),
		"DELETE": deleteTriggerDDL(),
	} {
		if !validGuardTrigger(event, statement) {
			t.Fatalf("valid %s trigger rejected", event)
		}
	}
	if validGuardTrigger("UPDATE", "SIGNAL SQLSTATE '45000'; -- pulse_user_center") {
		t.Fatal("incomplete update trigger accepted")
	}
	lookalike := `BEGIN
	  IF 0 THEN
	    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Meta Pulse binding is immutable';
	  END IF;
	  SET @fragments = 'pulse_user_center old.provider new.provider old.user_id new.user_id old.external_id new.external_id';
	END`
	if validGuardTrigger("UPDATE", lookalike) {
		t.Fatal("lookalike update trigger accepted")
	}
	weakenedGrouping := strings.ReplaceAll(updateTriggerDDL(),
		"IF (OLD.provider = 'pulse_user_center' OR NEW.provider = 'pulse_user_center') AND NOT (",
		"IF OLD.provider = 'pulse_user_center' OR NEW.provider = 'pulse_user_center' AND NOT (")
	if validGuardTrigger("UPDATE", weakenedGrouping) {
		t.Fatal("update trigger with weakened boolean grouping accepted")
	}
}

func TestDisposableForumDSNRejectsBusinessSchemas(t *testing.T) {
	for _, tc := range []struct {
		dsn  string
		want bool
	}{
		{"root:secret@tcp(127.0.0.1:3306)/pulse_integration?parseTime=true", true},
		{"root:secret@tcp(127.0.0.1:3306)/forum-test?parseTime=true", true},
		{"root:secret@tcp(127.0.0.1:3306)/meta_pulse_forum?parseTime=true", false},
		{"invalid", false},
	} {
		if got := disposableForumDSN(tc.dsn); got != tc.want {
			t.Errorf("disposableForumDSN(%q)=%v want %v", tc.dsn, got, tc.want)
		}
	}
}

func disposableForumDSN(raw string) bool {
	cfg, err := mysqlDriver.ParseDSN(strings.TrimSpace(raw))
	if err != nil || cfg.DBName == "" {
		return false
	}
	name := strings.ToLower(cfg.DBName)
	return strings.HasSuffix(name, "_integration") || strings.HasSuffix(name, "-integration") ||
		strings.HasSuffix(name, "_test") || strings.HasSuffix(name, "-test")
}

func requireDisposableForumDSN(t *testing.T, envName string) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(envName))
	if dsn == "" {
		t.Skip(envName + " is not set")
	}
	if !disposableForumDSN(dsn) {
		t.Fatalf("%s must select a disposable schema ending in _integration, -integration, _test, or -test", envName)
	}
	return dsn
}

func TestMySQLBindingGuardEnforcesOneToOneImmutableBinding(t *testing.T) {
	dsn := requireDisposableForumDSN(t, "FORUM_BINDING_GUARD_INTEGRATION_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS user_external_login"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS user_external_login") })
	if _, err := db.ExecContext(ctx, `
CREATE TABLE user_external_login (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  user_id BIGINT NOT NULL DEFAULT 0,
  provider VARCHAR(100) NOT NULL DEFAULT '',
  external_id VARCHAR(128) NOT NULL DEFAULT '',
  meta_info TEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		t.Fatal(err)
	}

	guard, err := NewMySQLBindingGuard(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := guard.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := guard.Ensure(ctx); err != nil {
		t.Fatalf("idempotent ensure failed: %v", err)
	}
	if err := guard.Ready(ctx); err != nil {
		t.Fatal(err)
	}

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustFail := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err == nil {
			t.Fatalf("unsafe SQL succeeded: %s", query)
		}
	}
	mustExec("INSERT INTO user_external_login(user_id, provider, external_id) VALUES(?, ?, ?)", 10, pluginSlug, "101")
	mustFail("INSERT INTO user_external_login(user_id, provider, external_id) VALUES(?, ?, ?)", 11, pluginSlug, "101")
	mustFail("INSERT INTO user_external_login(user_id, provider, external_id) VALUES(?, ?, ?)", 10, pluginSlug, "102")
	mustFail("INSERT INTO user_external_login(user_id, provider, external_id) VALUES(?, ?, ?)", 12, pluginSlug, "001")
	mustFail("INSERT INTO user_external_login(user_id, provider, external_id) VALUES(?, ?, ?)", 12, pluginSlug, "18446744073709551616")
	mustFail("UPDATE user_external_login SET user_id = 11 WHERE provider = ? AND external_id = ?", pluginSlug, "101")
	mustFail("UPDATE user_external_login SET provider = 'other' WHERE provider = ? AND external_id = ?", pluginSlug, "101")
	mustFail("UPDATE user_external_login SET created_at = created_at - INTERVAL 1 DAY WHERE provider = ? AND external_id = ?", pluginSlug, "101")
	mustFail("DELETE FROM user_external_login WHERE provider = ? AND external_id = ?", pluginSlug, "101")
	mustExec("UPDATE user_external_login SET meta_info = 'refreshed' WHERE provider = ? AND external_id = ?", pluginSlug, "101")

	// Conditional generated-column indexes must not alter other connectors.
	mustExec("INSERT INTO user_external_login(user_id, provider, external_id) VALUES(20, 'other', 'same'), (20, 'other', 'same')")
	mustExec("DELETE FROM user_external_login WHERE provider = 'other'")

	// A same-named composite index weakens one-to-one uniqueness and must not
	// pass the runtime readiness check.
	mustExec("DROP INDEX " + bindingGuardExternalIndex + " ON user_external_login")
	mustExec("CREATE UNIQUE INDEX " + bindingGuardExternalIndex + " ON user_external_login (" + bindingGuardExternalColumn + ", user_id)")
	if err := guard.Ready(ctx); err == nil {
		t.Fatal("weakened composite binding index was reported ready")
	}
	mustExec("DROP INDEX " + bindingGuardExternalIndex + " ON user_external_login")
	mustExec("CREATE UNIQUE INDEX " + bindingGuardExternalIndex + " ON user_external_login (" + bindingGuardExternalColumn + ")")

	mustExec("DROP TRIGGER " + bindingGuardDeleteTrigger)
	if err := guard.Ready(ctx); err == nil {
		t.Fatal("missing immutability trigger was reported ready")
	}
}
