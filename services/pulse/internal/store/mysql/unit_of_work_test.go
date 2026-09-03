package mysql

import (
	"context"
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func TestUnitOfWorkRejectsUninitializedState(t *testing.T) {
	var unit *UnitOfWork
	if err := unit.Do(context.Background(), func(ports.Repositories) error { return nil }); err == nil {
		t.Fatal("nil unit of work accepted")
	}
	if _, err := NewUnitOfWork(nil); err == nil {
		t.Fatal("nil database accepted")
	}
}
