package mysql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/runtimeconfig"
	"github.com/nanashiwang/meta-pulse/migrations"
)

func runtimeIntegrationConfig(role string) config.Config {
	cfg := config.Config{Environment: "production", HTTPAddr: ":8088", WorkerHTTPAddr: ":8089", PulseDBDSN: "test-only", RedisAddr: "test-only", IngestBatchSize: 250, SettlementBatchSize: 100, PeriodCloseBatchSize: 20, ContentIngestBatchSize: 100, ContentMaxUserPeriodAmount: 100, ContentMaxDailyAmount: 1000, TicketThresholdMilli: 1000, RewardRandomSecret: strings.Repeat("random", 8), RewardShadowMode: true, QuotaPerUnit: 500000, NewAPIInternalURL: "http://new-api:3000", NewAPILogDSN: "test-only"}
	if role == runtimeconfig.RoleAPI {
		cfg.ForumHMACSecret = strings.Repeat("forum", 8)
		cfg.UserBFFHMACSecret = strings.Repeat("user", 10)
		cfg.AdminHMACSecret = strings.Repeat("admin", 8)
		cfg.CommunityBFFHMACSecret = strings.Repeat("community", 5)
		cfg.RollbackHMACSecret = strings.Repeat("rollback", 5)
	} else {
		cfg.ServiceHMACSecret = strings.Repeat("service", 6)
	}
	return cfg
}

func TestMySQLRuntimeSettingsAtomicReplayIsolationAndRecovery(t *testing.T) {
	dsn := os.Getenv("PULSE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set PULSE_INTEGRATION_DSN to an isolated disposable schema")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid integration DSN")
	}
	if !(strings.HasSuffix(parsed.DBName, "_integration") || strings.HasSuffix(parsed.DBName, "-integration") || strings.HasSuffix(parsed.DBName, "_test") || strings.HasSuffix(parsed.DBName, "-test")) {
		t.Fatal("runtime integration requires a disposable schema ending in _integration, -integration, _test or -test")
	}
	database, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migrations.Up(ctx, database.SQL()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"pulse_runtime_change", "pulse_runtime_secret", "pulse_runtime_role"} {
		if _, err := database.SQL().ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.SQL().ExecContext(ctx, "UPDATE pulse_runtime_config SET revision=0, config_json=JSON_OBJECT() WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	actor := fmt.Sprintf("runtime-integration-%d", time.Now().UnixNano())
	store := NewRuntimeConfigStore(database)
	apiDir, workerDir := t.TempDir(), t.TempDir()
	missingSecret := runtimeIntegrationConfig(runtimeconfig.RoleAPI)
	missingSecret.AdminHMACSecret = ""
	if _, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleAPI, apiDir, missingSecret); !errors.Is(err, runtimeconfig.ErrInvalid) {
		t.Fatalf("invalid initial role registration succeeded: %v", err)
	}
	var registered int
	if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_runtime_role").Scan(&registered); err != nil || registered != 0 {
		t.Fatalf("invalid bootstrap persisted baseline: count=%d error=%v", registered, err)
	}
	api, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleAPI, apiDir, runtimeIntegrationConfig(runtimeconfig.RoleAPI))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleWorker, workerDir, runtimeIntegrationConfig(runtimeconfig.RoleWorker))
	if err != nil {
		t.Fatal(err)
	}
	view, err := api.View(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !view.NewAPITargetLocked || !view.WorkerReady {
		t.Fatal("registered worker not reflected in safe view")
	}
	apiSecret := strings.Repeat("new-api-admin", 4)
	workerSecret := strings.Repeat("new-worker-settlement", 3)
	request := runtimeconfig.UpdateRequest{Revision: view.Revision, Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": apiSecret, "PULSE_SERVICE_HMAC_SECRET": workerSecret}, Reason: "integration: initial signing key update"}
	saved, err := api.Update(ctx, request, actor, "first-update")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 100)
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := api.Update(ctx, request, actor, "first-update")
			if err != nil {
				errorsCh <- err
			} else if !reflect.DeepEqual(got, saved) {
				errorsCh <- errors.New("replayed response changed")
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	var count int
	if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_audit_log WHERE actor_id = ? AND action='runtime_settings.update'", actor).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replay audit count=%d error=%v", count, err)
	}
	if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_runtime_change WHERE actor_id = ?", actor).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replay receipt count=%d error=%v", count, err)
	}
	request.Reason = "different payload"
	if _, err := api.Update(ctx, request, actor, "first-update"); !errors.Is(err, runtimeconfig.ErrConflict) {
		t.Fatalf("changed payload error=%v", err)
	}
	if _, err := api.Update(ctx, request, actor, "stale-new-key"); !errors.Is(err, runtimeconfig.ErrConflict) {
		t.Fatalf("stale CAS error=%v", err)
	}
	apiRecord, err := store.Read(ctx, runtimeconfig.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	workerRecord, err := store.Read(ctx, runtimeconfig.RoleWorker)
	if err != nil {
		t.Fatal(err)
	}
	if len(apiRecord.Secrets["PULSE_SERVICE_HMAC_SECRET"].Ciphertext) != 0 || len(workerRecord.Secrets["PULSE_ADMIN_HMAC_SECRET"].Ciphertext) != 0 {
		t.Fatal("store leaked another role's ciphertext")
	}
	apiCurrent, err := api.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workerCurrent, err := worker.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if apiCurrent.AdminHMACSecret != apiSecret || apiCurrent.ServiceHMACSecret != "" || workerCurrent.ServiceHMACSecret != workerSecret || workerCurrent.AdminHMACSecret != "" || workerCurrent.RollbackHMACSecret != "" {
		t.Fatal("runtime plaintext role isolation failed")
	}
	rows, err := database.SQL().QueryContext(ctx, "SELECT ciphertext FROM pulse_runtime_secret")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ciphertext []byte
		if err := rows.Scan(&ciphertext); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(ciphertext, []byte(apiSecret)) || bytes.Contains(ciphertext, []byte(workerSecret)) {
			t.Fatal("database contains plaintext credential")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var before, after []byte
	if err := database.SQL().QueryRowContext(ctx, "SELECT before_json,after_json FROM pulse_audit_log WHERE actor_id=? ORDER BY id DESC LIMIT 1", actor).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	for _, disallowed := range []string{apiSecret, workerSecret, apiRecord.Secrets["PULSE_ADMIN_HMAC_SECRET"].Fingerprint, apiRecord.Secrets["PULSE_SERVICE_HMAC_SECRET"].Fingerprint} {
		if bytes.Contains(before, []byte(disallowed)) || bytes.Contains(after, []byte(disallowed)) {
			t.Fatal("audit contains secret material")
		}
	}
	if !bytes.Contains(after, []byte("PULSE_SERVICE_HMAC_SECRET")) {
		t.Fatal("audit missing changed field")
	}

	t.Run("concurrent CAS commits exactly one revision", func(t *testing.T) {
		current, err := api.View(ctx)
		if err != nil {
			t.Fatal(err)
		}
		success := make(chan struct{}, 20)
		errs := make(chan error, 20)
		for i := range 20 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				quota := fmt.Sprintf("%d", 500001+i)
				_, err := api.Update(ctx, runtimeconfig.UpdateRequest{Revision: current.Revision, Config: &runtimeconfig.Patch{QuotaPerUnit: &quota}, Reason: "integration concurrent CAS"}, actor, fmt.Sprintf("cas-%d", i))
				if err == nil {
					success <- struct{}{}
				} else if !errors.Is(err, runtimeconfig.ErrConflict) {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(success)
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if len(success) != 1 {
			t.Fatalf("CAS writers=%d", len(success))
		}
		got, err := api.View(ctx)
		if err != nil || got.Revision != current.Revision+1 {
			t.Fatalf("CAS revision=%d error=%v", got.Revision, err)
		}
	})

	t.Run("audit failure rolls back config ciphertext and replay receipt", func(t *testing.T) {
		current, err := api.View(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.SQL().ExecContext(ctx, "CREATE TRIGGER trg_runtime_integration_audit_fail BEFORE INSERT ON pulse_audit_log FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='runtime integration injected audit failure'"); err != nil {
			t.Fatal(err)
		}
		defer database.SQL().ExecContext(ctx, "DROP TRIGGER IF EXISTS trg_runtime_integration_audit_fail")
		changed := strings.Repeat("rolled-back-secret", 3)
		req := runtimeconfig.UpdateRequest{Revision: current.Revision, Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": changed}, Reason: "integration audit rollback"}
		if _, err := api.Update(ctx, req, actor, "audit-rollback"); err == nil {
			t.Fatal("injected audit failure succeeded")
		}
		got, err := api.View(ctx)
		if err != nil || got.Revision != current.Revision {
			t.Fatalf("failed write changed revision error=%v", err)
		}
		cfg, err := api.Current(ctx)
		if err != nil || cfg.AdminHMACSecret != apiSecret {
			t.Fatal("failed audit committed ciphertext")
		}
		if _, err := database.SQL().ExecContext(ctx, "DROP TRIGGER trg_runtime_integration_audit_fail"); err != nil {
			t.Fatal(err)
		}
		if _, err := api.Update(ctx, req, actor, "audit-rollback"); err != nil {
			t.Fatalf("failed mutation consumed idempotency key: %v", err)
		}
	})

	t.Run("restart restores secrets and missing key fails closed", func(t *testing.T) {
		restarted, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleAPI, apiDir, runtimeIntegrationConfig(runtimeconfig.RoleAPI))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := restarted.Current(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleAPI, t.TempDir(), runtimeIntegrationConfig(runtimeconfig.RoleAPI)); !errors.Is(err, runtimeconfig.ErrKeyMismatch) {
			t.Fatalf("lost key did not fail closed: %v", err)
		}
		if _, err := api.Current(ctx); err != nil {
			t.Fatalf("failed key registration changed valid recipient: %v", err)
		}
		moved := runtimeIntegrationConfig(runtimeconfig.RoleWorker)
		moved.NewAPIInternalURL = "http://different-funds-source:3000"
		workerRestart, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleWorker, workerDir, moved)
		if err != nil {
			t.Fatal(err)
		}
		pinned, err := workerRestart.Current(ctx)
		if err != nil || pinned.NewAPIInternalURL != "http://new-api:3000" {
			t.Fatalf("environment moved frozen receiver: %q error=%v", pinned.NewAPIInternalURL, err)
		}
		var bootstrapCount int
		if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_audit_log WHERE action='runtime_settings.bootstrap' AND resource_id='2'").Scan(&bootstrapCount); err != nil || bootstrapCount < 1 {
			t.Fatalf("bootstrap receiver pin missing audit: count=%d error=%v", bootstrapCount, err)
		}
		changed := runtimeIntegrationConfig(runtimeconfig.RoleAPI)
		changed.AdminHMACSecret = strings.Repeat("changed-env-admin", 3)
		if _, err := runtimeconfig.New(ctx, store, runtimeconfig.RoleAPI, apiDir, changed); !errors.Is(err, runtimeconfig.ErrConflict) {
			t.Fatalf("inconsistent environment registered: %v", err)
		}
	})

	t.Run("null stored configuration never restores environment", func(t *testing.T) {
		var original []byte
		if err := database.SQL().QueryRowContext(ctx, "SELECT config_json FROM pulse_runtime_config WHERE id=1").Scan(&original); err != nil {
			t.Fatal(err)
		}
		if _, err := database.SQL().ExecContext(ctx, "UPDATE pulse_runtime_config SET config_json='null' WHERE id=1"); err != nil {
			t.Fatal(err)
		}
		defer database.SQL().ExecContext(ctx, "UPDATE pulse_runtime_config SET config_json=? WHERE id=1", original)
		if _, err := api.Current(ctx); err == nil {
			t.Fatal("invalid persisted object fell back to environment")
		}
	})
}
