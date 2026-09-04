package migrations

import (
	"math"
	"strings"
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
	if len(found) != 9 {
		t.Fatalf("found %d migrations, want 9", len(found))
	}
	for index, migration := range found {
		want := int64(index + 1)
		if migration.Version != want {
			t.Fatalf("migration[%d] version=%d, want %d", index, migration.Version, want)
		}
	}
}

func TestTerminalConflictMigrationCannotRequeueConflictsOnDown(t *testing.T) {
	payload, err := FS.ReadFile("00008_settlement_terminal_conflict.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(payload), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatalf("unexpected migration sections: %d", len(parts))
	}
	down := strings.ToLower(parts[1])
	if strings.Contains(down, "set status = 'dead'") || strings.Contains(down, "set status='dead'") {
		t.Fatal("down migration makes terminal conflicts reconcilable again")
	}
}
