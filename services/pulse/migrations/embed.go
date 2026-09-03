// Package migrations embeds the audited SQL schema so the operator tool can
// migrate consistently in local, CI, and container environments.
package migrations

import (
	"context"
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

// FS contains versioned SQL migrations. README.md is intentionally excluded.
//
//go:embed *.sql
var FS embed.FS

func Up(ctx context.Context, db *sql.DB) error {
	if err := goose.SetDialect("mysql"); err != nil {
		return err
	}
	goose.SetBaseFS(FS)
	return goose.UpContext(ctx, db, ".")
}

func Status(ctx context.Context, db *sql.DB) error {
	if err := goose.SetDialect("mysql"); err != nil {
		return err
	}
	goose.SetBaseFS(FS)
	return goose.StatusContext(ctx, db, ".")
}
