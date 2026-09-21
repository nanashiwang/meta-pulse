package service

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"time"
)

// Shared by the displayed probability table and the actual draw.
func ticketActionPeriod(ctx context.Context, repos ports.Repositories, current period.Period, userID uint64, now time.Time) (period.Period, *ports.TicketLot, error) {
	if repos.Tickets == nil {
		return current, nil, errors.New("ticket repository unavailable")
	}
	if err := repos.Tickets.LockUser(ctx, userID); err != nil {
		return current, nil, err
	}
	// Preserve previously issued legacy tickets under their original expiry/rules.
	accounts, err := repos.Account.ListForUser(ctx, userID)
	if err != nil {
		return current, nil, err
	}
	for _, a := range accounts {
		if a.AssetType != ledger.AssetTicket || a.Balance <= 0 {
			continue
		}
		p, err := repos.Tickets.Period(ctx, a.PeriodID)
		if err != nil {
			return current, nil, err
		}
		if !p.Continuous && p.Contains(now) {
			return p, nil, nil
		}
	}
	lot, err := repos.Tickets.Next(ctx, userID)
	if err != nil {
		return current, nil, err
	}
	if lot == nil {
		return current, nil, ErrInsufficientTickets
	}
	p, err := repos.Tickets.Period(ctx, lot.PeriodID)
	if err != nil {
		return current, nil, err
	}
	if !p.Continuous || p.Status != period.StatusActive {
		return current, nil, ErrActionsUnavailable
	}
	return p, lot, nil
}
