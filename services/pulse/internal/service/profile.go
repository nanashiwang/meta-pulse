package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type UserProfile struct {
	UserID               uint64
	LifetimeContribution money.Milli
	Level                level.Result
}

type ProfileService struct {
	unit ports.UnitOfWork
}

func NewProfileService(unit ports.UnitOfWork, levels []level.Definition) (*ProfileService, error) {
	if unit == nil {
		return nil, errors.New("profile unit of work is nil")
	}
	return &ProfileService{unit: unit}, nil
}

func (s *ProfileService) Get(ctx context.Context, userID uint64) (UserProfile, error) {
	if userID == 0 {
		return UserProfile{}, fmt.Errorf("%w: invalid user id", ledger.ErrInvalidEntry)
	}
	// Compatibility fields are neutral; historical accounts remain audit-only.
	return UserProfile{UserID: userID, Level: level.Result{Key: "retired", Name: "已停用"}}, nil
}
