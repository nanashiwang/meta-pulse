package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// PulseSummary is the read-only product view used by the new-api BFF. It is
// deliberately assembled from Pulse facts and never accepts a client user ID.
type PulseSummary struct {
	Profile              UserProfile
	CurrentPeriod        *period.Period
	CurrentContribution  money.Milli
	AvailableTickets     int64
	CurrentLedgerEntries []ledger.Entry
	LedgerHasMore        bool
}

// SummaryLedgerLimit bounds the product projection, never the financial facts
// used for balances, reconciliation or account reconstruction.
const SummaryLedgerLimit = 100

func (s *ProfileService) GetSummary(ctx context.Context, userID uint64, at time.Time) (PulseSummary, error) {
	if userID == 0 {
		return PulseSummary{}, fmt.Errorf("%w: invalid user id", ledger.ErrInvalidEntry)
	}
	if at.IsZero() {
		at = time.Now()
	}
	var result PulseSummary
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Account == nil || repos.Period == nil || repos.Ledger == nil {
			return errors.New("summary repositories are not initialized")
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
			total, err = total.Add(money.Milli(account.Balance))
			if err != nil {
				return err
			}
		}
		result.Profile = UserProfile{UserID: userID, LifetimeContribution: total, Level: level.Calculate(total, s.levels)}

		active, periodErr := repos.Period.FindActiveAt(ctx, at)
		if errors.Is(periodErr, period.ErrNoActivePeriod) {
			return nil
		}
		if periodErr != nil {
			return periodErr
		}
		result.CurrentPeriod = &active
		for _, account := range accounts {
			if account.PeriodID != active.ID {
				if !active.Continuous || repos.Tickets == nil {
					continue
				}
				p, err := repos.Tickets.Period(ctx, account.PeriodID)
				if err != nil {
					return err
				}
				if !p.Continuous && !p.Contains(at) {
					continue
				}
			}
			switch account.AssetType {
			case ledger.AssetContribution:
				result.CurrentContribution, err = result.CurrentContribution.Add(money.Milli(account.Balance))
				if err != nil {
					return err
				}
			case ledger.AssetTicket:
				// Ticket debt is retained in the ledger so refunds cannot be
				// abused, but the product-facing available count must never
				// expose that debt as spendable tickets.
				if account.Balance > 0 {
					sum, err := money.Milli(result.AvailableTickets).Add(money.Milli(account.Balance))
					if err != nil {
						return err
					}
					result.AvailableTickets = int64(sum)
				}
			}
		}
		contributionEntries, err := repos.Ledger.ListRecentAccountEntries(ctx, userID, active.ID, ledger.AssetContribution, SummaryLedgerLimit+1)
		if err != nil {
			return err
		}
		ticketEntries, err := repos.Ledger.ListRecentAccountEntries(ctx, userID, active.ID, ledger.AssetTicket, SummaryLedgerLimit+1)
		if err != nil {
			return err
		}
		result.CurrentLedgerEntries = append(contributionEntries, ticketEntries...)
		sort.SliceStable(result.CurrentLedgerEntries, func(i, j int) bool {
			return result.CurrentLedgerEntries[i].ID < result.CurrentLedgerEntries[j].ID
		})
		if len(result.CurrentLedgerEntries) > SummaryLedgerLimit {
			result.LedgerHasMore = true
			result.CurrentLedgerEntries = result.CurrentLedgerEntries[len(result.CurrentLedgerEntries)-SummaryLedgerLimit:]
		}
		return nil
	})
	return result, err
}
