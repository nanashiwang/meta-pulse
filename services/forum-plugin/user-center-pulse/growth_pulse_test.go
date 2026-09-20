package pulse_user_center

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/growth"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMySQLExperienceSyncAcknowledgementRecovery(t *testing.T) {
	dsn := os.Getenv("FORUM_GROWTH_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("FORUM_GROWTH_INTEGRATION_DSN required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("disposable _test database required")
	}
	cfg.ParseTime = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE IF NOT EXISTS `user` (id BIGINT PRIMARY KEY,username VARCHAR(60),status INT,mail_status INT,created_at TIMESTAMP)"); err != nil {
		t.Fatal(err)
	}
	user := fmt.Sprint(time.Now().UnixNano())
	if _, err = db.Exec("INSERT INTO `user` VALUES(?,?,1,1,UTC_TIMESTAMP())", user, "sync"+user); err != nil {
		t.Fatal(err)
	}
	store := growth.New(db)
	if err = store.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	state := "pending"
	failAck := true
	amount := int64(500)
	grant := "sync-" + user
	_, uc, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get(pulseHeaderUserID) != "42" || r.Header.Get(pulseHeaderRole) != "community-bff" || r.Header.Get(pulseHeaderSignature) == "" {
			t.Error("untrusted identity or missing service signature")
		}
		switch r.URL.Path {
		case "/v1/internal/me/experience":
			items := []map[string]any{}
			if state != "" {
				items = append(items, map[string]any{"grant_id": grant, "amount": amount, "status": state})
			}
			json.NewEncoder(w).Encode(map[string]any{"deliveries": items})
		case "/v1/internal/me/experience/ack":
			var b map[string]string
			if json.NewDecoder(r.Body).Decode(&b) != nil || b["grant_id"] != grant || b["status"] != state || len(b) != 2 {
				t.Error("wrong acknowledgement")
			}
			if failAck {
				w.WriteHeader(503)
				return
			}
			state = ""
			w.Write([]byte(`{"ok":true}`))
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	uc.growthStore = store
	balance := func(want int64) {
		t.Helper()
		s, e := store.Summary(context.Background(), user)
		if e != nil || s.Balance != want {
			t.Fatalf("balance %d want %d: %v", s.Balance, want, e)
		}
	}
	if err = uc.syncPulseExperience(context.Background(), user); err == nil {
		t.Fatal("failed ACK must remain pending")
	}
	balance(500)
	mu.Lock()
	failAck = false
	mu.Unlock()
	if err = uc.syncPulseExperience(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	balance(500)
	if guard.seen != user {
		t.Fatal("binding was not checked")
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM metar_exp_ledger WHERE user_id=?", user).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate award", count, err)
	}
	mu.Lock()
	state = "pending"
	amount = 501
	mu.Unlock()
	if err = uc.syncPulseExperience(context.Background(), user); err == nil {
		t.Fatal("changed amount accepted")
	}
	balance(500)
	mu.Lock()
	state = "reversed"
	amount = 500
	mu.Unlock()
	if err = uc.syncPulseExperience(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	balance(0)
	mu.Lock()
	state = "pending"
	mu.Unlock()
	// A stale award after a completed reversal cannot resurrect experience.
	_ = uc.syncPulseExperience(context.Background(), user)
	balance(0)
	if err = db.QueryRow("SELECT COUNT(*) FROM metar_exp_ledger WHERE user_id=?", user).Scan(&count); err != nil || count != 2 {
		t.Fatal("reversal ledger", count, err)
	}
}
