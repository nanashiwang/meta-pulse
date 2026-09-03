// Package mysql owns Pulse's database connection. It never opens or writes to
// new-api's database.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	maxOpenConns    = 20
	maxIdleConns    = 10
	connMaxLifetime = 30 * time.Minute
)

type DB struct {
	gorm *gorm.DB
	sql  *sql.DB
}

func Open(dsn string) (*DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("pulse database DSN is empty")
	}
	gormDB, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{DisableAutomaticPing: true, TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open pulse database: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("get pulse database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	return &DB{gorm: gormDB, sql: sqlDB}, nil
}

func (db *DB) SQL() *sql.DB {
	if db == nil {
		return nil
	}
	return db.sql
}

func (db *DB) GORM() *gorm.DB {
	if db == nil {
		return nil
	}
	return db.gorm
}

func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.sql == nil {
		return fmt.Errorf("pulse database is not initialized")
	}
	return db.sql.PingContext(ctx)
}

func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}
