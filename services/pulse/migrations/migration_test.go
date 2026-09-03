package migrations

import (
	"math"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrationsAreDiscoverable(t *testing.T) {
	goose.SetBaseFS(FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })

	found, err := goose.CollectMigrations(".", 0, math.MaxInt64)
	if err != nil {
		t.Fatalf("collect migrations: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d migrations, want 2", len(found))
	}
	if found[0].Version != 1 || found[1].Version != 2 {
		t.Fatalf("migration versions = %d/%d, want 1/2", found[0].Version, found[1].Version)
	}
}
