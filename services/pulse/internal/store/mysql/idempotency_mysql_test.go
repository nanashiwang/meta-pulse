package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/migrations"
)

func TestMySQLIdempotencyExistingKeyReturnsCurrentCommittedReceipt(t *testing.T) {
	dsn := os.Getenv("PULSE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set PULSE_INTEGRATION_DSN to an isolated disposable schema")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = migrations.Up(ctx, db.SQL()); err != nil {
		t.Fatal(err)
	}
	unit, _ := NewUnitOfWork(db)
	scope, key, hash := "current_read_test", fmt.Sprint(time.Now().UnixNano()), strings.Repeat("a", 64)
	defer db.SQL().Exec("DELETE FROM pulse_idempotency WHERE scope=? AND idempotency_key=?", scope, key)
	if err = unit.Do(ctx, func(r ports.Repositories) error {
		_, e := r.Idempotency.GetOrCreateForUpdate(ctx, scope, key, hash)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// Establish an old repeatable-read snapshot before another transaction saves
	// the successful response. The locator may see old data; the lock must not.
	tx := db.GORM().WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var before idempotencyModel
	if err = tx.Where("scope=? AND idempotency_key=?", scope, key).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before.ResponseStatus != nil {
		t.Fatal("fixture already completed")
	}
	if err = unit.Do(ctx, func(r ports.Repositories) error {
		record, e := r.Idempotency.GetOrCreateForUpdate(ctx, scope, key, hash)
		if e != nil {
			return e
		}
		status := 200
		record.ResponseStatus = &status
		record.ResponseJSON = []byte(`{"entry_id":42}`)
		record.ResourceType = "ledger_entry"
		record.ResourceID = "42"
		return r.Idempotency.Save(ctx, record)
	}); err != nil {
		t.Fatal(err)
	}
	current, err := (&idempotencyRepository{db: tx}).GetOrCreateForUpdate(ctx, scope, key, strings.Repeat("b", 64))
	if err != nil || current.ResponseStatus == nil || *current.ResponseStatus != 200 || !json.Valid(current.ResponseJSON) || current.ResourceID != "42" || current.PayloadHash != hash {
		t.Fatalf("stale or overwritten receipt: %+v %v", current, err)
	}
}
