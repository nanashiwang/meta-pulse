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
	if len(found) != 5 {
		t.Fatalf("found %d migrations, want 5", len(found))
	}
	if found[0].Version != 1 || found[1].Version != 2 || found[2].Version != 3 || found[3].Version != 4 || found[4].Version != 5 {
		t.Fatalf("migration versions = %d/%d/%d/%d/%d, want 1/2/3/4/5", found[0].Version, found[1].Version, found[2].Version, found[3].Version, found[4].Version)
	}
}
