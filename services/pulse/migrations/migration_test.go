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
	if len(found) != 8 {
		t.Fatalf("found %d migrations, want 8", len(found))
	}
	for index, migration := range found {
		want := int64(index + 1)
		if migration.Version != want {
			t.Fatalf("migration[%d] version=%d, want %d", index, migration.Version, want)
		}
	}
}
