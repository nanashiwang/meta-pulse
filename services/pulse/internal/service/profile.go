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
	unit   ports.UnitOfWork
	levels []level.Definition
}

func NewProfileService(unit ports.UnitOfWork, levels []level.Definition) (*ProfileService, error) {
	if unit == nil {
		return nil, errors.New("profile unit of work is nil")
	}
	return &ProfileService{unit: unit, levels: append([]level.Definition(nil), levels...)}, nil
}

func (s *ProfileService) Get(ctx context.Context, userID uint64) (UserProfile, error) {
	if userID == 0 {
		return UserProfile{}, fmt.Errorf("%w: invalid user id", ledger.ErrInvalidEntry)
	}
	var result UserProfile
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Account == nil {
			return errors.New("account repository is not initialized")
		}
		accounts, err := repos.Account.ListForUser(ctx, userID)
		if err != nil {
			return err
		}
		var total money.Milli
		for _, account := range accounts {
			if account.AssetType != ledger.AssetContribution {
				continue
			}
			var addErr error
			total, addErr = total.Add(money.Milli(account.Balance))
			if addErr != nil {
				return addErr
			}
		}
		result = UserProfile{UserID: userID, LifetimeContribution: total, Level: level.Calculate(total, s.levels)}
		return nil
	})
	return result, err
}
