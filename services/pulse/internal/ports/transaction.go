// Package ports contains interfaces owned by the application layer. Concrete
// database types must not leak into domain or service code.
package ports

import "context"

type Repositories struct {
	Ledger  LedgerRepository
	Account AccountRepository
}

// UnitOfWork is the only transaction boundary exposed to services. The
// callback receives repositories bound to one Pulse DB transaction.
type UnitOfWork interface {
	Do(context.Context, func(Repositories) error) error
}
