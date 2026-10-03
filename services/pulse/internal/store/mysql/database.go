// Package mysql owns Pulse's database connection. It never opens or writes to
// new-api's database.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/dbconfig"
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
	normalized, err := dbconfig.Normalize(dsn)
	if err != nil {
		return nil, err
	}
	options, err := poolOptionsFromEnv()
	if err != nil {
		return nil, err
	}
	gormDB, err := gorm.Open(gormmysql.Open(normalized), &gorm.Config{DisableAutomaticPing: true, TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open pulse database: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("get pulse database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(options.open)
	sqlDB.SetMaxIdleConns(options.idle)
	sqlDB.SetConnMaxLifetime(options.lifetime)
	sqlDB.SetConnMaxIdleTime(options.idleTime)
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

type poolOptions struct {
	open, idle         int
	lifetime, idleTime time.Duration
}

func poolOptionsFromEnv() (poolOptions, error) {
	p := poolOptions{maxOpenConns, maxIdleConns, connMaxLifetime, 5 * time.Minute}
	for key, target := range map[string]*int{"PULSE_DB_MAX_OPEN_CONNS": &p.open, "PULSE_DB_MAX_IDLE_CONNS": &p.idle} {
		if value := os.Getenv(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > 1000 {
				return p, fmt.Errorf("invalid %s", key)
			}
			*target = n
		}
	}
	for key, target := range map[string]*time.Duration{"PULSE_DB_CONN_MAX_LIFETIME": &p.lifetime, "PULSE_DB_CONN_MAX_IDLE_TIME": &p.idleTime} {
		if value := os.Getenv(key); value != "" {
			n, err := time.ParseDuration(value)
			if err != nil || n <= 0 {
				return p, fmt.Errorf("invalid %s", key)
			}
			*target = n
		}
	}
	if p.open < 1 || p.idle > p.open {
		return p, errors.New("invalid Pulse database pool limits")
	}
	return p, nil
}
