package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/service"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

// Only SELECT: never initialize or lock a cursor, or contact new-api.
func runIngestSnapshot() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	item := service.CursorOverviewItem{Name: service.DefaultUsageCursorName, SourceSystem: "new-api-log"}
	err = db.SQL().QueryRowContext(ctx, `SELECT cursor_value, version, watermark_at FROM pulse_worker_cursor WHERE cursor_name = ? AND source_system = ?`, item.Name, item.SourceSystem).Scan(&item.Value, &item.Version, &item.WatermarkAt)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if item.WatermarkAt != nil {
		item.LagSeconds = int64(time.Since(*item.WatermarkAt).Seconds())
		if item.LagSeconds < 0 {
			item.LagSeconds = 0
		}
	}
	return json.NewEncoder(os.Stdout).Encode(item)
}
