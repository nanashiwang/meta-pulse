// Package ports contains interfaces owned by the application layer. Concrete
// database types must not leak into domain or service code.
package ports

import "context"

type Repositories struct {
	Ledger      LedgerRepository
	Account     AccountRepository
	Usage       UsageRepository
	Conflict    ConflictRepository
	Cursor      CursorRepository
	Period      PeriodRepository
	Economics   EconomicsRepository
	UserPeriod  UserPeriodStatRepository
	Reward      RewardRepository
	Idempotency IdempotencyRepository
	Settlement  SettlementRepository
	PeriodAdmin PeriodAdminRepository
	Audit       AuditRepository
	Experiment  ExperimentRepository
	Metric      MetricRepository
	Operations  OperationsRepository
}

// UnitOfWork is the only transaction boundary exposed to services. The
// callback receives repositories bound to one Pulse DB transaction.
type UnitOfWork interface {
	Do(context.Context, func(Repositories) error) error
}
