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
	if len(found) != 7 {
		t.Fatalf("found %d migrations, want 7", len(found))
	}
	if found[0].Version != 1 || found[1].Version != 2 || found[2].Version != 3 || found[3].Version != 4 || found[4].Version != 5 || found[5].Version != 6 || found[6].Version != 7 {
		t.Fatalf("migration versions = %d/%d/%d/%d/%d/%d/%d, want 1/2/3/4/5/6/7", found[0].Version, found[1].Version, found[2].Version, found[3].Version, found[4].Version, found[5].Version, found[6].Version)
	}
}
