package mysql

import (
	"context"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
)

// UnitOfWork binds all repositories added in later milestones to one Pulse
// transaction. It intentionally exposes no transaction handle to callers.
type UnitOfWork struct {
	db *gorm.DB
}

func NewUnitOfWork(db *DB) (*UnitOfWork, error) {
	if db == nil || db.GORM() == nil {
		return nil, fmt.Errorf("pulse database is not initialized")
	}
	return &UnitOfWork{db: db.GORM()}, nil
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	if u == nil || u.db == nil {
		return fmt.Errorf("unit of work is not initialized")
	}
	if fn == nil {
		return fmt.Errorf("unit of work callback is nil")
	}
	return u.db.WithContext(ctx).Transaction(func(_ *gorm.DB) error {
		return fn(ports.Repositories{})
	})
}
